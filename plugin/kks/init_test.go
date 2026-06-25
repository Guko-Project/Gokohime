package kks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseKKSAddArgs(t *testing.T) {
	category, playlistURL, err := parseKKSAddArgs("日推 https://music.163.com/playlist?id=123")
	if err != nil {
		t.Fatalf("parseKKSAddArgs failed: %v", err)
	}
	if category != "日推" || playlistURL != "https://music.163.com/playlist?id=123" {
		t.Fatalf("unexpected args: category=%q url=%q", category, playlistURL)
	}
}

func TestParseKKSAddArgsRejectsListCategory(t *testing.T) {
	if _, _, err := parseKKSAddArgs("list https://music.163.com/playlist?id=123"); err == nil {
		t.Fatalf("expected list category to be rejected")
	}
}

func TestParseKKSDelArgs(t *testing.T) {
	category, song, err := parseKKSDelArgs("日推 Little Wish")
	if err != nil {
		t.Fatalf("parseKKSDelArgs failed: %v", err)
	}
	if category != "日推" || song != "Little Wish" {
		t.Fatalf("unexpected args: category=%q song=%q", category, song)
	}

	category, song, err = parseKKSDelArgs("日推")
	if err != nil {
		t.Fatalf("parseKKSDelArgs category failed: %v", err)
	}
	if category != "日推" || song != "" {
		t.Fatalf("unexpected category delete args: category=%q song=%q", category, song)
	}
}

func TestExactKKSCommandTextDoesNotMatchLongerCommands(t *testing.T) {
	if isExactKKSCommandText(".kksadd 日推 https://example.com", ".", "kks") {
		t.Fatalf("expected .kksadd not to match .kks")
	}
	if isExactKKSCommandText(".kksdel 日推", ".", "kks") {
		t.Fatalf("expected .kksdel not to match .kks")
	}
	if !isExactKKSCommandText(".kks 日推", ".", "kks") {
		t.Fatalf("expected .kks with args to match")
	}
	if !isExactKKSCommandText(".kksadd 日推 https://example.com", ".", "kksadd") {
		t.Fatalf("expected .kksadd with args to match")
	}
	if !isExactKKSCommandText(".kksdel 日推", ".", "kksdel") {
		t.Fatalf("expected .kksdel with args to match")
	}
}

func TestFetchSonglist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("unexpected content-type: %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}
		if got := r.Form.Get("url"); got != "https://music.163.com/playlist?id=123" {
			t.Fatalf("unexpected url form value: %q", got)
		}
		_ = json.NewEncoder(w).Encode(songlistResponse{Code: 1, Data: struct {
			Name       string   `json:"name"`
			Songs      []string `json:"songs"`
			SongsCount int      `json:"songs_count"`
		}{
			Name:       "测试歌单",
			Songs:      []string{" Saika ", "", "雪影", "Saika"},
			SongsCount: 4,
		}})
	}))
	defer server.Close()

	restoreEndpoint := songlistEndpoint
	restoreClient := songlistHTTPClient
	songlistEndpoint = server.URL
	songlistHTTPClient = server.Client()
	t.Cleanup(func() {
		songlistEndpoint = restoreEndpoint
		songlistHTTPClient = restoreClient
	})

	songs, err := fetchSonglist(context.Background(), "https://music.163.com/playlist?id=123")
	if err != nil {
		t.Fatalf("fetchSonglist failed: %v", err)
	}
	if strings.Join(songs, ",") != "Saika,雪影,Saika" {
		t.Fatalf("unexpected songs: %#v", songs)
	}
}

func TestFetchSonglistHTTPTimeoutConfigured(t *testing.T) {
	if songlistHTTPClient.Timeout != 3*time.Minute {
		t.Fatalf("unexpected timeout: %s", songlistHTTPClient.Timeout)
	}
}

func TestFetchSonglistRejectsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, _ = url.Parse(r.Form.Get("url"))
		_ = json.NewEncoder(w).Encode(songlistResponse{Code: 0, Msg: "bad playlist"})
	}))
	defer server.Close()

	restoreEndpoint := songlistEndpoint
	restoreClient := songlistHTTPClient
	songlistEndpoint = server.URL
	songlistHTTPClient = server.Client()
	t.Cleanup(func() {
		songlistEndpoint = restoreEndpoint
		songlistHTTPClient = restoreClient
	})

	if _, err := fetchSonglist(context.Background(), "https://music.163.com/playlist?id=123"); err == nil || err.Error() != "bad playlist" {
		t.Fatalf("unexpected error: %v", err)
	}
}
