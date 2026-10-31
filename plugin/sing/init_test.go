package sing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/config"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type delivery struct {
	action   string
	target   int64
	segments message.Message
}
type caller struct {
	sent      chan delivery
	failAudio bool
}

func (c *caller) CallAPI(_ context.Context, req zero.APIRequest) (zero.APIResponse, error) {
	d := delivery{action: req.Action, segments: req.Params["message"].(message.Message)}
	if req.Action == "send_group_msg" {
		d.target = req.Params["group_id"].(int64)
	} else {
		d.target = req.Params["user_id"].(int64)
	}
	c.sent <- d
	if c.failAudio && d.segments[0].Type == "record" {
		return zero.APIResponse{}, nil
	}
	return zero.APIResponse{Data: gjson.Parse(`{"message_id":-12}`)}, nil
}
func receive(t *testing.T, c *caller) delivery {
	t.Helper()
	select {
	case d := <-c.sent:
		return d
	case <-time.After(3 * time.Second):
		t.Fatal("timed out awaiting message")
		return delivery{}
	}
}

func TestAsynchronousDelivery(t *testing.T) {
	for _, tc := range []struct{ command, mode string }{{"sing", "score"}, {"msing", "mimic"}, {"ssing", "score"}, {"song", "score"}} {
		for _, groupID := range []int64{123, 0} {
			t.Run(tc.command+fmt.Sprint(groupID), func(t *testing.T) {
				query, matchType, lyricQuery := "雨爱 杨丞琳", "title_artist", ""
				if groupID == 0 {
					query, matchType, lyricQuery = "迎着风向前走", "lyric", "迎着风向前走"
				}
				release := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/voices":
						fmt.Fprint(w, `{"voices":[{"id":"vocaloid-luotianyi","name":"洛天依","aliases":["洛"],"available":true}]}`)
					case "/resolve_song":
						<-release
						if requestQuery(t, r) != query {
							t.Error("wrong query")
						}
						json.NewEncoder(w).Encode(resolvedSong{Song: song{ID: "1", Name: "雨爱"}, MatchType: matchType, LyricQuery: lyricQuery})
					case "/release_task":
						var body struct {
							Start      *float64 `json:"start"`
							End        *float64 `json:"end"`
							Modes      []string `json:"modes"`
							LyricQuery string   `json:"lyric_query"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Modes) != 1 || body.Modes[0] != tc.mode {
							t.Errorf("wrong generation mode: %+v, %v", body, err)
						}
						if body.LyricQuery != lyricQuery {
							t.Errorf("wrong lyric query: %q", body.LyricQuery)
						}
						if tc.command == "song" {
							if body.Start == nil || body.End == nil || *body.Start != 0 || *body.End != 120 {
								t.Errorf("lost range: %+v", body)
							}
						} else if body.Start != nil || body.End != nil {
							t.Error("unexpected range")
						}
						fmt.Fprint(w, `{"task_id":"task"}`)
					case "/query_result":
						fmt.Fprintf(w, `{"status":"done","outputs":{%q:"/audio"}}`, tc.mode)
					case "/v1/audio":
						if r.URL.Query().Get("kind") != tc.mode {
							t.Errorf("wrong audio kind: %s", r.URL.Query().Get("kind"))
						}
						fmt.Fprint(w, "ID3voice")
					}
				}))
				defer server.Close()
				// Unblock the server even on a failed assertion.
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				path := filepath.Join(t.TempDir(), "config.yaml")
				os.WriteFile(path, []byte(fmt.Sprintf("sing:\n  api_key: test\n  base_url: %q\n", server.URL)), 0600)
				if _, err := config.Load(path); err != nil {
					t.Fatal(err)
				}
				c := &caller{sent: make(chan delivery, 10), failAudio: groupID == 0}
				zero.APICallers.Store(987654, c)
				defer zero.APICallers.Delete(987654)
				ctx := zero.GetBot(987654)
				ctx.Event = &zero.Event{SelfID: 987654, GroupID: groupID, UserID: 456}
				args := query
				if tc.command == "song" {
					args = "洛 " + query + " 0-120"
				}
				ctx.State = zero.State{"command": tc.command, "args": " " + args + " "}
				returned := make(chan struct{})
				go func() { handleSing(ctx); close(returned) }()
				first := receive(t, c)
				valid := false
				for _, text := range warmups {
					if first.segments[0].Data["text"] == text {
						valid = true
					}
				}
				if !valid {
					t.Fatalf("wrong warmup: %+v", first)
				}
				select {
				case <-returned:
				case <-time.After(time.Second):
					t.Fatal("handler blocked on generation")
				}
				handleSing(ctx)
				if !strings.Contains(receive(t, c).segments[0].Data["text"], "上一首") {
					t.Fatal("duplicate task accepted")
				}
				// Mutating the original event after returning must not reroute the result.
				ctx.Event.GroupID, ctx.Event.UserID = 999, 888
				ctx.State["args"] = "different song"
				ctx.State["command"] = "different command"
				close(release)
				if !strings.Contains(receive(t, c).segments[0].Data["text"], "洛天依") {
					t.Fatal("missing chosen voice")
				}
				audio := receive(t, c)
				wantAction, wantTarget := "send_group_msg", groupID
				if groupID == 0 {
					wantAction, wantTarget = "send_private_msg", 456
				}
				if audio.action != wantAction || audio.target != wantTarget {
					t.Fatalf("wrong target: %+v", audio)
				}
				if audio.segments[0].Type != "record" || audio.segments[0].Data["file"] != "base64://"+base64.StdEncoding.EncodeToString([]byte("ID3voice")) {
					t.Fatal("wrong record payload")
				}
				if c.failAudio && !strings.Contains(receive(t, c).segments[0].Data["text"], "语音发送失败") {
					t.Fatal("missing delivery error")
				}
				deadline := time.Now().Add(time.Second)
				for {
					if _, loaded := pending.Load(destination{987654, groupID, 456}); !loaded {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("pending request not cleared")
					}
					time.Sleep(time.Millisecond)
				}
			})
		}
	}
}

func TestCommandBoundary(t *testing.T) {
	for _, command := range []string{"sing", "msing", "ssing", "song", "voices"} {
		for _, tc := range []struct {
			suffix string
			want   bool
		}{{"", true}, {" 雨爱", true}, {"\t雨爱", true}, {"ing", false}, {"er", false}} {
			ctx := &zero.Ctx{State: zero.State{"command": command}, Event: &zero.Event{Message: message.Message{message.Text(zero.BotConfig.CommandPrefix + command + tc.suffix)}}}
			if commandBoundary(ctx) != tc.want {
				t.Errorf("boundary %q", tc.suffix)
			}
		}
	}
}
