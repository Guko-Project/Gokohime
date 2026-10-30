package guesssong

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/colanns/gokohime/internal/database"
)

func TestMusicBoxResolution(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		location string
		want     error
	}{
		{"redirect", 302, "https://example.com/full.mp3", nil},
		{"unavailable JSON", 200, "", errSongUnavailable},
		{"forbidden", 403, "", errSongUnavailable},
		{"rate limited", 429, "", errRateLimited},
		{"missing location", 302, "", errSongUnavailable},
		{"local file", 302, "file:///etc/passwd", errSongUnavailable},
		{"credentials", 302, "https://user:password@example.com/a", errSongUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/meting/" || r.URL.Query().Get("server") != "netease" || r.URL.Query().Get("type") != "url" || r.URL.Query().Get("id") != "1918884127" {
					t.Errorf("unexpected query: %s", r.URL)
				}
				if r.Header.Get("Origin") != "https://mu-jie.cc" || r.Header.Get("Referer") != "https://mu-jie.cc/" || r.Header.Get("Cookie") != "" {
					t.Error("unexpected request headers")
				}
				w.Header().Set("Location", tc.location)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"msg":"已重试，但仍无法获取"}`)
			}))
			defer server.Close()
			client := &neteaseClient{musicBoxURL: server.URL, http: server.Client()}
			play, err := client.resolveMusicBox(context.Background(), 1918884127, 187826)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil && (play.ID != 1918884127 || play.URL != tc.location) {
				t.Fatalf("bad playback: %+v", play)
			}
			// A caller's cancellation must survive source error wrapping.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := client.resolveMusicBox(ctx, 1918884127, 187826); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}

func TestMusicBoxPriorityAndDownloadFallback(t *testing.T) {
	audio := audioFixture(t)
	shortPath := filepath.Join(t.TempDir(), "trial.mp3")
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=10", "-c:a", "libmp3lame", "-b:a", "128k", shortPath).CombinedOutput(); err != nil {
		t.Fatalf("trial fixture: %v %s", err, out)
	}
	shortAudio, err := os.ReadFile(shortPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "unavailable", "rate limited", "bad audio", "short audio"} {
		t.Run(mode, func(t *testing.T) {
			var primary, legacy, downloads atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/meting/":
					primary.Add(1)
					if mode == "unavailable" {
						fmt.Fprint(w, `{"msg":"已重试，但仍无法获取"}`)
						return
					}
					if mode == "rate limited" {
						w.WriteHeader(429)
						return
					}
					http.Redirect(w, r, server.URL+"/primary", 302)
				case "/primary":
					downloads.Add(1)
					w.Header().Set("Content-Type", "audio/mpeg")
					switch mode {
					case "bad audio":
						fmt.Fprint(w, `{"error":"unavailable"}`)
					case "short audio":
						w.Write(shortAudio)
					default:
						w.Write(audio)
					}
				case "/api/song/enhance/player/url":
					legacy.Add(1)
					fmt.Fprintf(w, `{"code":200,"data":[{"id":1,"code":200,"url":%q,"time":20000}]}`, server.URL+"/legacy")
				case "/legacy":
					w.Header().Set("Content-Type", "audio/mpeg")
					w.Write(audio)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			svc := newSongService(t.TempDir(), &neteaseClient{musicBoxURL: server.URL, baseURL: server.URL, http: server.Client()})
			session, err := svc.prepareCandidates(context.Background(), 1, "group_1", "rv", []database.GuessSongCatalog{testTrack(1)})
			if err != nil {
				t.Fatal(err)
			}
			if session.SongID != 1 || primary.Load() != 1 {
				t.Fatal("failed to preserve song ID")
			}
			wantLegacy := int32(1)
			if mode == "success" {
				wantLegacy = 0
			}
			if legacy.Load() != wantLegacy {
				t.Fatalf("legacy requests=%d want=%d", legacy.Load(), wantLegacy)
			}
			if downloads.Load() > 1 {
				t.Fatal("resolver downloaded audio before downloader")
			}
			cached, err := os.ReadFile(filepath.Join(svc.root, "cache", "1.mp3"))
			if err != nil || len(cached) != len(audio) {
				t.Fatalf("bad cache: len=%d err=%v", len(cached), err)
			}
			files, _ := os.ReadDir(filepath.Join(svc.root, "cache"))
			if len(files) != 1 {
				t.Fatalf("temporary files left behind: %v", files)
			}
			server.Close()
			if _, err := svc.prepareCandidates(context.Background(), 2, "group_2", "rv", []database.GuessSongCatalog{testTrack(1)}); err != nil {
				t.Fatalf("offline cache reuse: %v", err)
			}
		})
	}
}

// Opt-in full download and slice test; normal tests never contact third parties.
func TestLiveMusicBox(t *testing.T) {
	if os.Getenv("GOKOHIME_MUSICBOX_LIVE") != "1" {
		t.Skip("live test disabled")
	}
	requireAudioTools(t)
	var catalog struct {
		Tracks []catalogTrack `json:"tracks"`
	}
	// Use the bundled metadata, exactly as a normal game does.
	data, err := os.ReadFile("catalog_rv.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1918884127, 548356} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			var entry database.GuessSongCatalog
			for _, track := range catalog.Tracks {
				if track.ID == id {
					meta, _ := json.Marshal(track)
					entry = database.GuessSongCatalog{SongID: id, Library: "rv", MetaJSON: string(meta)}
					break
				}
			}
			if entry.SongID == 0 {
				t.Fatal("missing catalog entry")
			}
			client := newNeteaseClient()
			// Disable the legacy resolver to prove that MusicBox supplied the audio.
			client.baseURL = "http://127.0.0.1:1"
			svc := newSongService(t.TempDir(), client)
			ctx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
			defer cancel()
			session, err := svc.prepare(ctx, 1, "group_1", "rv", entry)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("MusicBox full audio and two slices verified: %d %s", id, session.SongName)
		})
	}
}
