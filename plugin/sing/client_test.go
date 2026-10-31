package sing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientWorkflow(t *testing.T) {
	for _, mode := range []string{"score", "mimic"} {
		t.Run(mode, func(t *testing.T) {
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing auth")
				}
				if r.Header.Get("User-Agent") != "Gokohime/1.0" {
					t.Error("missing user agent")
				}
				switch r.URL.Path {
				case "/resolve_song":
					if requestQuery(t, r) != "雨爱 & live" {
						t.Error("query not preserved")
					}
					fmt.Fprint(w, `{"song":{"id":"chosen","name":"雨爱 & live","artists":["歌手"]},"match_type":"title"}`)
				case "/release_task":
					var body struct {
						SongID string   `json:"song_id"`
						Modes  []string `json:"modes"`
					}
					if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("bad submit")
					}
					if body.SongID != "chosen" || len(body.Modes) != 1 || body.Modes[0] != mode {
						t.Errorf("unexpected submit: %+v", body)
					}
					fmt.Fprint(w, `{"task_id":"task & 1"}`)
				case "/query_result":
					if r.URL.Query().Get("task_id") != "task & 1" {
						t.Error("task ID not preserved")
					}
					polls++
					switch polls {
					case 1:
						fmt.Fprint(w, `{"status":"queued"}`)
					case 2:
						w.WriteHeader(http.StatusServiceUnavailable)
					case 3:
						fmt.Fprint(w, `{"status":"running"}`)
					default:
						fmt.Fprint(w, `{"status":"done","outputs":{"score":"https://untrusted.example/audio","mimic":"/wrong"}}`)
					}
				case "/v1/audio":
					if r.URL.Query().Get("kind") != mode || r.URL.Query().Get("task_id") != "task & 1" {
						t.Error("wrong audio")
					}
					fmt.Fprint(w, "ID3score-audio")
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			c := newClient(server.URL, "secret", mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s, err := c.resolve(ctx, "雨爱 & live")
			if err != nil {
				t.Fatal(err)
			}
			id, err := c.submit(ctx, s.Song.ID, s.LyricQuery)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.wait(ctx, id, time.Millisecond); err != nil {
				t.Fatal(err)
			}
			audio, err := c.audio(ctx, id)
			if err != nil || string(audio) != "ID3score-audio" {
				t.Fatalf("audio=%q err=%v", audio, err)
			}
		})
	}
}

func TestClientFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		op         string
		want       error
	}{
		{"no songs", `{}`, 404, "resolve", errNoSong},
		{"empty song IDs", `{"song":{"name":"x"}}`, 200, "resolve", errNoSong},
		{"unauthorized", `secret diagnostic`, 401, "resolve", httpError(401)},
		{"missing task ID", `{}`, 200, "submit", nil},
		{"failed", `{"status":"failed","error":"sensitive diagnostic"}`, 200, "wait", errGeneration},
		{"missing score", `{"status":"done","outputs":{"mimic":"/mimic"}}`, 200, "wait", nil},
		{"unknown status", `{"status":"weird"}`, 200, "wait", nil},
		{"malformed JSON", `<html>bad</html>`, 200, "wait", nil},
		{"invalid audio", `<html>bad</html>`, 200, "audio", nil},
		{"empty audio", ``, 200, "audio", nil},
		{"oversize", ``, 200, "oversize", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.op == "oversize" {
					w.Header().Set("Content-Length", fmt.Sprint(maxAudioBytes+1))
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := newClient(server.URL, "secret", "score")
			var err error
			switch tc.op {
			case "resolve":
				_, err = c.resolve(context.Background(), "song")
			case "submit":
				_, err = c.submit(context.Background(), "song", "")
			case "wait":
				err = c.wait(context.Background(), "task", time.Millisecond)
			default:
				_, err = c.audio(context.Background(), "task")
			}
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("unexpected err: %v", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("error leaked response body")
			}
		})
	}
}

func TestResolveAndSubmitPreservesIntent(t *testing.T) {
	for _, tc := range []struct {
		query, matchType, lyricQuery string
	}{
		{"雨爱 杨丞琳", "title_artist", ""},
		{"Love Story Taylor Swift", "title_artist", ""},
		{"迎着风，向前走 & hello", "lyric", "迎着风，向前走 & hello"},
		{"没有命中", "fallback", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			submitted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/resolve_song":
					if requestQuery(t, r) != tc.query {
						t.Error("original input not preserved")
					}
					json.NewEncoder(w).Encode(resolvedSong{Song: song{ID: "chosen"}, MatchType: tc.matchType, LyricQuery: tc.lyricQuery})
				case "/release_task":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["song_id"] != "chosen" {
						t.Errorf("wrong song: %v", body)
					}
					if tc.lyricQuery == "" {
						if _, ok := body["lyric_query"]; ok {
							t.Error("title/fallback should not submit a lyric anchor")
						}
					} else if body["lyric_query"] != tc.lyricQuery {
						t.Error("lyric anchor lost")
					}
					submitted = true
					fmt.Fprint(w, `{"task_id":"task"}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			c := newClient(server.URL, "key", "score")
			selected, err := c.resolve(context.Background(), tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.submit(context.Background(), selected.Song.ID, selected.LyricQuery); err != nil {
				t.Fatal(err)
			}
			if !submitted {
				t.Fatal("task not submitted")
			}
		})
	}
}

func TestPollDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"queued"}`) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := newClient(server.URL, "key", "score").wait(ctx, "task", time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline: %v", err)
	}
}

func TestRedirectDoesNotForwardKey(t *testing.T) {
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, http.StatusFound) }))
	defer source.Close()
	if _, err := newClient(source.URL, "key", "score").audio(context.Background(), "task"); !errors.Is(err, httpError(302)) {
		t.Fatalf("expected redirect rejection: %v", err)
	}
}

func requestQuery(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Error("resolve must use POST")
	}
	var body struct {
		Q string `json:"q"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return body.Q
}
