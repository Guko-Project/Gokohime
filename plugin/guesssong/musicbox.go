package guesssong

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// MusicBox's netease source uses the same IDs as our catalogs. Its playback
// endpoint redirects to audio; a 200 JSON response means resolution failed.
// Resolve without following redirects so audio is downloaded only once, through
// the normal size, format and full-duration checks.
func (c *neteaseClient) resolveMusicBox(ctx context.Context, songID int64, _ int) (*playback, error) {
	query := url.Values{"server": {"netease"}, "type": {"url"}, "id": {fmt.Sprint(songID)}}
	req, err := musicRequest(ctx, c.musicBoxURL+"/meting/?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid MusicBox endpoint", errSongUnavailable)
	}
	req.Header.Set("Referer", "https://mu-jie.cc/")
	req.Header.Set("Origin", "https://mu-jie.cc")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: MusicBox request failed", errSongUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return nil, fmt.Errorf("%w: MusicBox returned no audio redirect (HTTP %d)", errSongUnavailable, resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("%w: invalid MusicBox audio URL", errSongUnavailable)
	}
	return &playback{ID: songID, Code: 200, URL: u.String()}, nil
}
