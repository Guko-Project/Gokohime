package guesssong

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/colanns/gokohime/internal/database"
)

func TestSongMetadataSchemas(t *testing.T) {
	for _, fields := range []string{
		`"duration":20000,"artists":[{"name":"洛天依"}]`,
		`"dt":20000,"ar":[{"name":"洛天依"}]`,
	} {
		t.Run(fields, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "" || r.URL.Query().Get("ids") != "[1]" {
					t.Error("unexpected request")
				}
				fmt.Fprintf(w, `{"code":200,"songs":[{"id":1,"name":"测试",%s}]}`, fields)
			}))
			defer server.Close()
			client := &neteaseClient{baseURL: server.URL, http: server.Client()}
			info, err := client.songInfo(context.Background(), database.GuessSongCatalog{SongID: 1})
			if err != nil {
				t.Fatal(err)
			}
			if info.Name != "测试" || info.Artist != "洛天依" || info.DurationMS != 20000 {
				t.Fatalf("bad metadata: %+v", info)
			}
		})
	}
}

func TestRejectUnavailablePlayback(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       error
	}{
		{"missing", `{"code":200,"data":[{"id":1,"code":404,"url":null}]}`, 200, errSongUnavailable},
		{"trial", `{"code":200,"data":[{"id":1,"code":200,"url":"https://example.com/a","freeTrialInfo":{"start":0,"end":30}}]}`, 200, errSongUnavailable},
		{"wrong id", `{"code":200,"data":[{"id":2,"code":200,"url":"https://example.com/a"}]}`, 200, errSongUnavailable},
		{"short", `{"code":200,"data":[{"id":1,"code":200,"url":"https://example.com/a","time":30000}]}`, 200, errSongUnavailable},
		{"invalid url", `{"code":200,"data":[{"id":1,"code":200,"url":"file:///etc/passwd"}]}`, 200, errSongUnavailable},
		{"api rate limit", `{"code":429}`, 200, errRateLimited},
		{"song rate limit", `{"code":200,"data":[{"id":1,"code":429}]}`, 200, errRateLimited},
		{"http rate limit", ``, 429, errRateLimited},
		{"html", `<html>login</html>`, 200, errSongUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			client := &neteaseClient{baseURL: server.URL, http: server.Client()}
			_, err := client.resolve(context.Background(), 1, 180000)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDownloadRejectsInvalidBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, checksum string
		size                              int64
	}{
		{"html", "<html>login</html>", "audio/mpeg", "", 0},
		{"json", `{"error":"login"}`, "application/octet-stream", "", 0},
		{"empty", "", "audio/mpeg", "", 0},
		{"truncated", "ID3\x00\x00\x00", "audio/mpeg", "", 100},
		{"checksum", "ID3\x00\x00\x00", "audio/mpeg", "incorrect", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := &neteaseClient{http: server.Client()}
			var dst bytes.Buffer
			err := client.download(context.Background(), &playback{URL: server.URL, Size: tc.size, MD5: tc.checksum}, &dst)
			if !errors.Is(err, errSongUnavailable) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
