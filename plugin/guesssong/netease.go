package guesssong

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/database"
)

var (
	errSongUnavailable = errors.New("song unavailable")
	errRateLimited     = errors.New("music API rate limited")
)

type neteaseClient struct {
	baseURL     string
	musicBoxURL string
	http        *http.Client
}

func newNeteaseClient() *neteaseClient {
	return &neteaseClient{
		baseURL:     "https://music.163.com",
		musicBoxURL: "https://fy-musicbox-api.mu-jie.cc",
		http:        &http.Client{Timeout: 25 * time.Second},
	}
}

type songInfo struct {
	Name       string
	Artist     string
	DurationMS int
}

type playback struct {
	ID        int64           `json:"id"`
	URL       string          `json:"url"`
	Code      int             `json:"code"`
	Size      int64           `json:"size"`
	MD5       string          `json:"md5"`
	Time      int             `json:"time"`
	FreeTrial json.RawMessage `json:"freeTrialInfo"`
}

func musicRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	return req, nil
}

func (c *neteaseClient) getJSON(ctx context.Context, path string, query url.Values, payload any) error {
	req, err := musicRequest(ctx, c.baseURL+path+"?"+query.Encode())
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: music API request failed", errSongUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: music API HTTP %d", errSongUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(payload); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: invalid music API response", errSongUnavailable)
	}
	return nil
}

func (c *neteaseClient) songInfo(ctx context.Context, entry database.GuessSongCatalog) (*songInfo, error) {
	var track catalogTrack
	// The bundled playlist provides metadata without making a per-game detail
	// request. Legacy catalogs containing only IDs are still supported.
	if json.Unmarshal([]byte(entry.MetaJSON), &track) == nil &&
		track.ID == entry.SongID && strings.TrimSpace(track.Name) != "" &&
		len(track.Artists) > 0 && track.DurationMS > 0 {
		return &songInfo{Name: track.Name, Artist: strings.Join(track.Artists, " / "), DurationMS: track.DurationMS}, nil
	}
	type artist struct {
		Name string `json:"name"`
	}
	var payload struct {
		Code  int `json:"code"`
		Songs []struct {
			ID       int64    `json:"id"`
			Name     string   `json:"name"`
			Duration int      `json:"duration"`
			DT       int      `json:"dt"`
			Artists  []artist `json:"artists"`
			AR       []artist `json:"ar"`
		} `json:"songs"`
	}
	query := url.Values{"ids": {fmt.Sprintf("[%d]", entry.SongID)}}
	if err := c.getJSON(ctx, "/api/song/detail/", query, &payload); err != nil {
		return nil, err
	}
	if payload.Code == 429 {
		return nil, errRateLimited
	}
	if payload.Code != 200 {
		return nil, fmt.Errorf("%w: song detail code %d", errSongUnavailable, payload.Code)
	}
	for _, song := range payload.Songs {
		if song.ID != entry.SongID || strings.TrimSpace(song.Name) == "" {
			continue
		}
		if song.Duration == 0 {
			song.Duration = song.DT
		}
		if len(song.Artists) == 0 {
			song.Artists = song.AR
		}
		names := make([]string, 0, len(song.Artists))
		for _, a := range song.Artists {
			if strings.TrimSpace(a.Name) != "" {
				names = append(names, a.Name)
			}
		}
		if len(names) == 0 || song.Duration <= 0 {
			return nil, fmt.Errorf("%w: incomplete song metadata", errSongUnavailable)
		}
		return &songInfo{Name: song.Name, Artist: strings.Join(names, " / "), DurationMS: song.Duration}, nil
	}
	return nil, fmt.Errorf("%w: song %d not found", errSongUnavailable, entry.SongID)
}

func (c *neteaseClient) resolve(ctx context.Context, songID int64, expectedMS int) (*playback, error) {
	var payload struct {
		Code int        `json:"code"`
		Data []playback `json:"data"`
	}
	query := url.Values{"ids": {fmt.Sprintf("[%d]", songID)}, "br": {"128000"}}
	if err := c.getJSON(ctx, "/api/song/enhance/player/url", query, &payload); err != nil {
		return nil, err
	}
	if payload.Code == 429 {
		return nil, errRateLimited
	}
	if payload.Code != 200 {
		return nil, fmt.Errorf("%w: playback API code %d", errSongUnavailable, payload.Code)
	}
	for _, item := range payload.Data {
		if item.ID != songID {
			continue
		}
		if item.Code == 429 {
			return nil, errRateLimited
		}
		trial := strings.TrimSpace(string(item.FreeTrial))
		if item.Code != 200 || item.URL == "" || (trial != "" && trial != "null") {
			return nil, fmt.Errorf("%w: no full playback available", errSongUnavailable)
		}
		if item.Time > 0 && !durationMatches(float64(item.Time)/1000, expectedMS) {
			return nil, fmt.Errorf("%w: playback duration mismatch", errSongUnavailable)
		}
		u, err := url.Parse(item.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("%w: invalid playback URL", errSongUnavailable)
		}
		return &item, nil
	}
	return nil, fmt.Errorf("%w: playback not found", errSongUnavailable)
}
