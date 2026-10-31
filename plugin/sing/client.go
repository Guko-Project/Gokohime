package sing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxAudioBytes = 32 << 20

var errNoSong = errors.New("no matching song")
var errGeneration = errors.New("synthesis failed")

type song struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Artists []string `json:"artists"`
}

type resolvedSong struct {
	Match struct {
		Start     float64 `json:"start"`
		End       float64 `json:"end"`
		LineCount int     `json:"line_count"`
	} `json:"match"`
	Song       song   `json:"song"`
	MatchType  string `json:"match_type"`
	LyricQuery string `json:"lyric_query"`
}

type taskResult struct {
	Song      song              `json:"song"`
	Stage     string            `json:"stage"`
	Error     string            `json:"error"`
	ErrorInfo *failureInfo      `json:"error_info"`
	Status    string            `json:"status"`
	Outputs   map[string]string `json:"outputs"`
}

type client struct {
	withAccompaniment bool
	baseURL           string
	apiKey            string
	mode              string
	start             *float64
	end               *float64
	voiceID           string
	parts             []string
	http              *http.Client
}

func newClient(baseURL, apiKey, mode string) *client {
	return &client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, mode: mode, voiceID: defaultVoiceID, http: &http.Client{
		Timeout: 60 * time.Second,
		// Never forward credentials to a redirect destination.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("DiffSinger HTTP %d", e) }

func (c *client) request(ctx context.Context, method, path string, body any, limit int64) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "Gokohime/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 422 && (path == "/parse_score" || path == "/release_score_task") {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var problem struct {
			Detail scoreValidationError `json:"detail"`
		}
		if json.Unmarshal(data, &problem) == nil && problem.Detail.Line > 0 && problem.Detail.Message != "" {
			return nil, &problem.Detail
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpError(resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("DiffSinger response too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("DiffSinger response too large")
	}
	return data, nil
}

func (c *client) json(ctx context.Context, method, path string, body, out any) error {
	limit := int64(1 << 20)
	if path == "/parse_score" {
		// The normalized notes and pronunciation preview expand the input text.
		limit = 4 << 20
	}
	data, err := c.request(ctx, method, path, body, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *client) resolve(ctx context.Context, query string) (resolvedSong, error) {
	var selected resolvedSong
	err := c.json(ctx, http.MethodPost, "/resolve_song", map[string]string{"q": query, "lyric_extent": "auto"}, &selected)
	if errors.Is(err, httpError(http.StatusUnprocessableEntity)) {
		return resolvedSong{}, errLyricMatch
	}
	if errors.Is(err, httpError(http.StatusNotFound)) {
		return resolvedSong{}, errNoSong
	}
	if err != nil {
		return resolvedSong{}, fmt.Errorf("resolve: %w", err)
	}
	if strings.TrimSpace(selected.Song.ID) == "" {
		return resolvedSong{}, errNoSong
	}
	return selected, nil
}

func (c *client) submit(ctx context.Context, songID string, lyricQuery string) (string, error) {
	var task struct {
		ID string `json:"task_id"`
	}
	err := c.json(ctx, http.MethodPost, "/release_task", struct {
		WithAccompaniment bool     `json:"with_accompaniment,omitempty"`
		Start             *float64 `json:"start,omitempty"`
		End               *float64 `json:"end,omitempty"`
		SongID            string   `json:"song_id"`
		Modes             []string `json:"modes"`
		LyricQuery        string   `json:"lyric_query,omitempty"`
		VoiceID           string   `json:"voice_id"`
		LyricExtent       string   `json:"lyric_extent"`
	}{c.withAccompaniment, c.start, c.end, songID, []string{c.mode}, lyricQuery, c.voiceID, "auto"}, &task)
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}
	if strings.TrimSpace(task.ID) == "" {
		return "", errors.New("DiffSinger returned an empty task ID")
	}
	return task.ID, nil
}

func (c *client) wait(ctx context.Context, taskID string, interval time.Duration) error {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	failures := 0
	for {
		var result taskResult
		err := c.json(ctx, http.MethodGet, "/query_result?"+url.Values{"task_id": {taskID}}.Encode(), nil, &result)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures++
			var status httpError
			var network *url.Error
			retryable := (errors.As(err, &status) && (status == 429 || status >= 500)) || errors.As(err, &network)
			if !retryable || failures >= 3 {
				return fmt.Errorf("poll: %w", err)
			}
		} else {
			failures = 0
			switch result.Status {
			case "done":
				c.parts = nil
				for kind := range result.Outputs {
					suffix, ok := strings.CutPrefix(kind, c.outputKind()+"_part_")
					if ok && len(suffix) >= 4 && strings.Trim(suffix, "0123456789") == "" {
						c.parts = append(c.parts, kind)
					}
				}
				sort.Strings(c.parts)
				if result.Outputs[c.outputKind()] == "" {
					return fmt.Errorf("DiffSinger task has no %s audio", c.outputKind())
				}
				return nil
			case "failed":
				info := result.ErrorInfo
				if info == nil {
					legacy := legacyFailure(result.Error, result.Stage)
					info = &legacy
				}
				name := result.Song.Name
				if name == "" && result.Song.ID != "" {
					name = "歌曲 " + result.Song.ID
				}
				return &generationError{Info: *info, SongName: name}
			case "queued", "running":
			default:
				return fmt.Errorf("unknown DiffSinger task status %q", result.Status)
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *client) outputKind() string {
	if c.withAccompaniment {
		return c.mode + "_mix"
	}
	return c.mode
}

func (c *client) audio(ctx context.Context, taskID string) ([]byte, error) {
	return c.audioKind(ctx, taskID, c.outputKind())
}

func (c *client) audioKind(ctx context.Context, taskID, kind string) ([]byte, error) {
	// Build the authenticated URL locally, rather than trusting a returned URL.
	data, err := c.request(ctx, http.MethodGet, "/v1/audio?"+url.Values{"task_id": {taskID}, "kind": {kind}}.Encode(), nil, maxAudioBytes)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	// The API serves MP3. Reject HTML error pages and empty/truncated responses.
	if len(data) < 3 || !(bytes.HasPrefix(data, []byte("ID3")) || (data[0] == 0xff && data[1]&0xe0 == 0xe0)) {
		return nil, errors.New("DiffSinger returned invalid MP3 audio")
	}
	return data, nil
}
