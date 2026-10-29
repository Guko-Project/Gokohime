package guesssong

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
)

const (
	maxAudioBytes   = 40 << 20
	maxSongAttempts = 5
	songRetryAfter  = 30 * time.Minute
	prepareTimeout  = 90 * time.Second
)

var (
	errAudioTools   = errors.New("ffmpeg/ffprobe unavailable")
	errSongDownload = errors.New("song download failed")
)

type songService struct {
	root   string
	client *neteaseClient
	// Fixed-size locks bound memory use and avoid duplicate downloads across
	// groups. Waiting on a lock respects the game's preparation deadline.
	locks       [64]chan struct{}
	mu          sync.Mutex
	failedUntil map[int64]time.Time
}

func newSongService(root string, client *neteaseClient) *songService {
	s := &songService{root: root, client: client, failedUntil: make(map[int64]time.Time)}
	for i := range s.locks {
		s.locks[i] = make(chan struct{}, 1)
		s.locks[i] <- struct{}{}
	}
	return s
}

func (s *songService) coolingDown(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until := s.failedUntil[id]
	if !until.After(time.Now()) {
		delete(s.failedUntil, id)
		return false
	}
	return true
}

func (s *songService) prepareCandidates(ctx context.Context, sessionID int64, sessionDir, library string, candidates []database.GuessSongCatalog) (*database.GuessGameSession, error) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%w: %s", errAudioTools, tool)
		}
	}
	attempts := 0
	for _, entry := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.coolingDown(entry.SongID) {
			continue
		}
		attempts++
		session, err := s.prepare(ctx, sessionID, sessionDir, library, entry)
		if err == nil {
			return session, nil
		}
		log.Warnf("[guesssong] prepare failed: session=%d library=%s song=%d: %v", sessionID, library, entry.SongID, err)
		if !errors.Is(err, errSongUnavailable) {
			return nil, err
		}
		s.mu.Lock()
		s.failedUntil[entry.SongID] = time.Now().Add(songRetryAfter)
		s.mu.Unlock()
		if attempts >= maxSongAttempts {
			break
		}
	}
	return nil, fmt.Errorf("%w: no playable candidate after %d attempts", errSongUnavailable, attempts)
}

func (s *songService) prepare(ctx context.Context, sessionID int64, sessionDir, library string, entry database.GuessSongCatalog) (*database.GuessGameSession, error) {
	if entry.SongID <= 0 {
		return nil, fmt.Errorf("%w: invalid song ID", errSongUnavailable)
	}
	info, err := s.client.songInfo(ctx, entry)
	if err != nil {
		return nil, err
	}
	release, err := s.lockSong(ctx, entry.SongID)
	if err != nil {
		return nil, err
	}
	defer release()
	path, duration, err := s.cachedAudioLocked(ctx, entry.SongID, info.DurationMS)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSongDownload, err)
	}
	dir := filepath.Join(s.root, "sessions", sessionDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			s.removeSessionFiles(sessionDir)
		}
	}()
	first, second := chooseSliceOffsets(int(duration * 1000))
	slice1, slice2 := filepath.Join(dir, "slice_1.wav"), filepath.Join(dir, "slice_2.wav")
	if err := cutAudio(ctx, path, slice1, first); err != nil {
		return nil, err
	}
	if err := cutAudio(ctx, path, slice2, second); err != nil {
		return nil, err
	}
	completed = true
	return &database.GuessGameSession{
		GroupID: sessionID, Library: library, SongID: entry.SongID,
		SongName: info.Name, Artist: info.Artist,
		// Playback URLs expire; persist the canonical song page instead.
		AudioURL:   fmt.Sprintf("https://music.163.com/song?id=%d", entry.SongID),
		Slice1Path: slice1, Slice2Path: slice2,
		Answer: info.Name, PreparedAt: time.Now(), Active: true,
	}, nil
}

func durationMatches(seconds float64, expectedMS int) bool {
	return expectedMS <= 0 || math.Abs(seconds-float64(expectedMS)/1000) <= math.Max(3, float64(expectedMS)/1000*0.05)
}

func (s *songService) lockSong(ctx context.Context, songID int64) (func(), error) {
	lock := s.locks[uint64(songID)%uint64(len(s.locks))]
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
	}
	return func() { lock <- struct{}{} }, nil
}

func (s *songService) cachedAudio(ctx context.Context, songID int64, expectedMS int) (string, float64, error) {
	release, err := s.lockSong(ctx, songID)
	if err != nil {
		return "", 0, err
	}
	defer release()
	return s.cachedAudioLocked(ctx, songID, expectedMS)
}

// Caller holds the song lock until cutting is complete; cleanup cannot unlink
// audio that another goroutine is currently reading.
func (s *songService) cachedAudioLocked(ctx context.Context, songID int64, expectedMS int) (string, float64, error) {
	dir := filepath.Join(s.root, "cache")
	path := filepath.Join(dir, strconv.FormatInt(songID, 10)+".mp3")
	if stat, err := os.Stat(path); err == nil {
		if stat.Mode().IsRegular() && stat.Size() > 0 && stat.Size() <= maxAudioBytes {
			duration, probeErr := probeAudio(ctx, path)
			if probeErr == nil && duration >= 2*sliceLengthSec && durationMatches(duration, expectedMS) {
				now := time.Now()
				if err := os.Chtimes(path, now, now); err != nil {
					return "", 0, err
				}
				return path, duration, nil
			}
			if ctx.Err() != nil {
				return "", 0, ctx.Err()
			}
		}
		// A truncated/corrupt cached file must never become a permanent failure.
		if err := os.Remove(path); err != nil {
			return "", 0, err
		}
	} else if !os.IsNotExist(err) {
		return "", 0, err
	}
	play, err := s.client.resolve(ctx, songID, expectedMS)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	f, err := os.CreateTemp(dir, ".download-*.mp3")
	if err != nil {
		return "", 0, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := s.client.download(ctx, play, f); err != nil {
		f.Close()
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	duration, err := probeAudio(ctx, tmp)
	if err != nil {
		return "", 0, err
	}
	if duration < 2*sliceLengthSec || !durationMatches(duration, expectedMS) {
		return "", 0, fmt.Errorf("%w: downloaded audio duration mismatch", errSongUnavailable)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", 0, err
	}
	return path, duration, nil
}

func (c *neteaseClient) download(ctx context.Context, play *playback, dst io.Writer) error {
	req, err := musicRequest(ctx, play.URL)
	if err != nil {
		return fmt.Errorf("%w: invalid audio URL", errSongUnavailable)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: audio request failed", errSongUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: download audio status %d", errSongUnavailable, resp.StatusCode)
	}
	if resp.ContentLength > maxAudioBytes || play.Size > maxAudioBytes {
		return fmt.Errorf("%w: audio exceeds size limit", errSongUnavailable)
	}
	reader := bufio.NewReader(resp.Body)
	prefix, _ := reader.Peek(512)
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	sniffed := http.DetectContentType(prefix)
	if strings.Contains(contentType, "text/") || strings.Contains(contentType, "json") ||
		strings.HasPrefix(sniffed, "text/") || strings.HasPrefix(strings.TrimSpace(string(prefix)), "{") {
		return fmt.Errorf("%w: response is not audio", errSongUnavailable)
	}
	hash := md5.New()
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(reader, maxAudioBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var fileErr *os.PathError
		if errors.As(err, &fileErr) {
			return fmt.Errorf("write audio cache: %w", err)
		}
		return fmt.Errorf("%w: interrupted audio download", errSongUnavailable)
	}
	if n == 0 || n > maxAudioBytes || (play.Size > 0 && n != play.Size) {
		return fmt.Errorf("%w: incomplete or oversized audio", errSongUnavailable)
	}
	if play.MD5 != "" && !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), play.MD5) {
		return fmt.Errorf("%w: audio checksum mismatch", errSongUnavailable)
	}
	return nil
}

func probeAudio(ctx context.Context, path string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "a:0",
		"-show_entries", "format=duration:stream=codec_type", "-of", "json", path)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: ffprobe cannot decode audio", errSongUnavailable)
	}
	var payload struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(out, &payload) != nil || len(payload.Streams) == 0 || payload.Streams[0].CodecType != "audio" {
		return 0, fmt.Errorf("%w: no audio stream", errSongUnavailable)
	}
	duration, err := strconv.ParseFloat(payload.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return 0, fmt.Errorf("%w: invalid audio duration", errSongUnavailable)
	}
	return duration, nil
}

func cutAudio(ctx context.Context, inputPath, outputPath string, startSec int) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	f, err := os.CreateTemp(filepath.Dir(outputPath), ".slice-*.wav")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-protocol_whitelist", "file,pipe", "-ss", strconv.Itoa(startSec), "-i", inputPath,
		"-t", strconv.Itoa(sliceLengthSec), "-vn", "-ac", "1", "-ar", "24000", "-c:a", "pcm_s16le", tmp)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg slice failed: %w", err)
	}
	duration, err := probeAudio(ctx, tmp)
	if err != nil {
		return err
	}
	if math.Abs(duration-sliceLengthSec) > 0.1 {
		return fmt.Errorf("%w: incomplete audio slice", errSongUnavailable)
	}
	return os.Rename(tmp, outputPath)
}
