package guesssong

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/database"
)

func requireAudioTools(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s unavailable", name)
		}
	}
}

func testTrack(id int64) database.GuessSongCatalog {
	meta, _ := json.Marshal(catalogTrack{ID: id, Name: "测试曲", Artists: []string{"洛天依"}, DurationMS: 20000})
	return database.GuessSongCatalog{Library: "cv", SongID: id, MetaJSON: string(meta)}
}

func audioFixture(t *testing.T) []byte {
	t.Helper()
	requireAudioTools(t)
	path := filepath.Join(t.TempDir(), "fixture.mp3")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=20", "-c:a", "libmp3lame", "-b:a", "128k", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOnDemandCacheAndSlices(t *testing.T) {
	audio := audioFixture(t)
	var resolutions, downloads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("unexpected cookie")
		}
		switch r.URL.Path {
		case "/api/song/enhance/player/url":
			resolutions.Add(1)
			fmt.Fprintf(w, `{"code":200,"data":[{"id":1,"code":200,"url":%q,"size":%d,"md5":"%x","time":20000}]}`, server.URL+"/audio", len(audio), md5.Sum(audio))
		case "/audio":
			downloads.Add(1)
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(audio)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "guesssong")
	svc := newSongService(root, &neteaseClient{baseURL: server.URL, http: server.Client()})
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("service initialization created cache")
	}
	// Simultaneous games must share one download but get separate slices.
	var wg sync.WaitGroup
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session, err := svc.prepareCandidates(context.Background(), int64(i), fmt.Sprintf("group_%d", i), "cv", []database.GuessSongCatalog{testTrack(1)})
			if err != nil {
				t.Error(err)
				return
			}
			for _, path := range []string{session.Slice1Path, session.Slice2Path} {
				d, err := probeAudio(context.Background(), path)
				if err != nil || math.Abs(d-6) > .1 {
					t.Errorf("slice: duration=%v error=%v", d, err)
				}
			}
		}(i)
	}
	wg.Wait()
	if resolutions.Load() != 1 || downloads.Load() != 1 {
		t.Fatalf("resolve=%d download=%d", resolutions.Load(), downloads.Load())
	}
	// A damaged cached file should be replaced, never reused.
	if err := os.WriteFile(filepath.Join(root, "cache", "1.mp3"), []byte("<html>corrupt</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.cachedAudio(context.Background(), 1, 20000); err != nil {
		t.Fatal(err)
	}
	if resolutions.Load() != 2 || downloads.Load() != 2 {
		t.Fatal("did not repair cache")
	}
	server.Close()
	// No playback API or CDN is available now; a fresh service still reuses cache.
	restarted := newSongService(root, svc.client)
	if _, err := restarted.prepareCandidates(context.Background(), 3, "group_3", "cv", []database.GuessSongCatalog{testTrack(1)}); err != nil {
		t.Fatalf("offline cache hit: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "cache", "*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("unexpected cache files: %v, %v", files, err)
	}
}

func TestFailedCandidatesAreBoundedAndDoNotCache(t *testing.T) {
	requireAudioTools(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "guesssong")
	svc := newSongService(root, &neteaseClient{baseURL: server.URL, http: server.Client()})
	var candidates []database.GuessSongCatalog
	for i := 1; i <= 8; i++ {
		candidates = append(candidates, testTrack(int64(i)))
	}
	got, err := svc.prepareCandidates(context.Background(), 1, "group_1", "cv", candidates)
	if got != nil || !errors.Is(err, errSongUnavailable) || requests.Load() != maxSongAttempts {
		t.Fatalf("got=%v err=%v requests=%d", got, err, requests.Load())
	}
	_, _ = svc.prepareCandidates(context.Background(), 1, "group_1", "cv", candidates[:maxSongAttempts])
	if requests.Load() != maxSongAttempts {
		t.Fatal("unavailable songs were retried during cooldown")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("failed resolution created cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = svc.cachedAudio(ctx, 9, 20000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestFailedDownloadFallsBackWithoutCachingBadAudio(t *testing.T) {
	audio := audioFixture(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/song/enhance/player/url":
			var ids []int64
			if err := json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids); err != nil || len(ids) != 1 {
				t.Error("bad request")
				w.WriteHeader(400)
				return
			}
			fmt.Fprintf(w, `{"code":200,"data":[{"id":%d,"code":200,"url":%q,"size":%d}]}`, ids[0], server.URL+fmt.Sprintf("/audio/%d", ids[0]), len(audio))
		case "/audio/1":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(audio[:600]) // truncated MP3: never publish this cache entry
		case "/audio/2":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(audio)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	svc := newSongService(root, &neteaseClient{baseURL: server.URL, http: server.Client()})
	session, err := svc.prepareCandidates(context.Background(), 1, "group_1", "cv", []database.GuessSongCatalog{testTrack(1), testTrack(2)})
	if err != nil {
		t.Fatal(err)
	}
	if session.SongID != 2 {
		t.Fatal("did not fall back to playable song")
	}
	files, err := os.ReadDir(filepath.Join(root, "cache"))
	if err != nil || len(files) != 1 || files[0].Name() != "2.mp3" {
		t.Fatalf("invalid cache contents: %v %v", files, err)
	}
}

// Explicit opt-in; regular tests never contact music services or download songs.
func TestLiveNetease(t *testing.T) {
	if os.Getenv("GOKOHIME_GUESSSONG_LIVE") != "1" {
		t.Skip("live test disabled")
	}
	requireAudioTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
	defer cancel()
	svc := newSongService(t.TempDir(), newNeteaseClient())
	svc.client.musicBoxURL = "" // This test specifically exercises the direct NetEase source.
	entry := database.GuessSongCatalog{SongID: 3395708493}
	started := time.Now()
	session, err := svc.prepare(ctx, 1, "group_1", "cv", entry)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("downloaded and sliced: %s / %s in %s", session.SongName, session.Artist, time.Since(started))
	// Persisted metadata + cached audio must suffice without the network.
	meta, err := svc.client.songInfo(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(catalogTrack{ID: entry.SongID, Name: meta.Name, Artists: []string{meta.Artist}, DurationMS: meta.DurationMS})
	entry.MetaJSON = string(encoded)
	svc.client = &neteaseClient{baseURL: "http://127.0.0.1:1", http: &http.Client{Timeout: time.Second}}
	if _, err := svc.prepare(ctx, 2, "group_2", "cv", entry); err != nil {
		t.Fatalf("offline reuse: %v", err)
	}
	t.Log("second game reused cache with network disabled")
}
