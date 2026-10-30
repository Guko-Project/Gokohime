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

type taskResult struct {
	Status  string            `json:"status"`
	Outputs map[string]string `json:"outputs"`
}

type client struct {
	baseURL string
	apiKey  string
	mode    string
	http    *http.Client
}

func newClient(baseURL, apiKey, mode string) *client {
	return &client{strings.TrimRight(baseURL, "/"), apiKey, mode, &http.Client{
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
	data, err := c.request(ctx, method, path, body, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *client) search(ctx context.Context, query string) (song, error) {
	var songs []song
	err := c.json(ctx, http.MethodGet, "/search?"+url.Values{"q": {query}, "limit": {"10"}}.Encode(), nil, &songs)
	if err != nil {
		return song{}, fmt.Errorf("search: %w", err)
	}
	var first song
	for _, s := range songs {
		if strings.TrimSpace(s.ID) == "" {
			continue
		}
		if first.ID == "" {
			first = s
		}
		if strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(query)) {
			return s, nil
		}
	}
	if first.ID == "" {
		return song{}, errNoSong
	}
	return first, nil
}

func (c *client) submit(ctx context.Context, songID string) (string, error) {
	var task struct {
		ID string `json:"task_id"`
	}
	err := c.json(ctx, http.MethodPost, "/release_task", struct {
		SongID string   `json:"song_id"`
		Modes  []string `json:"modes"`
	}{songID, []string{c.mode}}, &task)
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
				if result.Outputs[c.mode] == "" {
					return fmt.Errorf("DiffSinger task has no %s audio", c.mode)
				}
				return nil
			case "failed":
				return errGeneration
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

func (c *client) audio(ctx context.Context, taskID string) ([]byte, error) {
	// Build the authenticated URL locally, rather than trusting a returned URL.
	data, err := c.request(ctx, http.MethodGet, "/v1/audio?"+url.Values{"task_id": {taskID}, "kind": {c.mode}}.Encode(), nil, maxAudioBytes)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	// The API serves MP3. Reject HTML error pages and empty/truncated responses.
	if len(data) < 3 || !(bytes.HasPrefix(data, []byte("ID3")) || (data[0] == 0xff && data[1]&0xe0 == 0xe0)) {
		return nil, errors.New("DiffSinger returned invalid MP3 audio")
	}
	return data, nil
}
