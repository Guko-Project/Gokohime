package sing

import (
	"context"
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
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestScoreArgumentsAndSinger(t *testing.T) {
	for _, raw := range []string{"\n1 2\n天晴", "\r\n1 2\r\n天晴"} {
		singer, text := splitScoreArgs(raw)
		if singer != "" || text != "1 2\n天晴" {
			t.Fatalf("lost newline: %q %q", singer, text)
		}
	}
	catalog := voiceCatalog{Voices: []voice{{ID: defaultVoiceID, Name: "洛天依", Aliases: []string{"洛"}, Available: true}, {ID: "miku", Aliases: []string{"初"}, Available: false}}}
	for _, name := range []string{"", "洛", "洛天依", defaultVoiceID} {
		v, err := scoreVoice(name, catalog)
		if err != nil || v.ID != defaultVoiceID {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if _, err := scoreVoice("未知", catalog); err != errScoreSinger {
		t.Fatal(err)
	}
	if _, err := scoreVoice("初", catalog); err != errVoiceUnavailable {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "\n1 2\n天晴", " 洛\n1\n天"} {
		ctx := &zero.Ctx{State: zero.State{"command": "score"}, Event: &zero.Event{Message: message.Message{message.Text(zero.BotConfig.CommandPrefix + "score" + suffix)}}}
		if !commandBoundary(ctx) {
			t.Fatal("score boundary rejected")
		}
	}
}

func TestScoreDelivery(t *testing.T) {
	for _, singer := range []string{"", "洛", "未知", "错误"} {
		t.Run(singer, func(t *testing.T) {
			released := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer score-test" {
					t.Error("missing auth")
				}
				switch r.URL.Path {
				case "/voices":
					fmt.Fprint(w, `{"voices":[{"id":"vocaloid-luotianyi","name":"洛天依","aliases":["洛","错误"],"available":true}]}`)
				case "/parse_score", "/release_score_task":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["voice_id"] != defaultVoiceID || body["text"] != "1 2\n天晴" {
						t.Errorf("bad score payload: %v", body)
					}
					if singer == "错误" {
						w.WriteHeader(422)
						fmt.Fprint(w, `{"detail":{"line":2,"column":1,"message":"需要 2 个演唱音节"}}`)
					} else if r.URL.Path == "/parse_score" {
						fmt.Fprint(w, `{"duration_seconds":45}`)
					} else {
						released = true
						fmt.Fprint(w, `{"task_id":"score-task"}`)
					}
				case "/query_result":
					fmt.Fprint(w, `{"status":"done","outputs":{"score":"x","score_part_0001":"x","score_part_0000":"x"}}`)
				case "/v1/audio":
					if !strings.HasPrefix(r.URL.Query().Get("kind"), "score_part_") {
						t.Error("should deliver parts")
					}
					fmt.Fprint(w, "ID3score")
				default:
					t.Errorf("unexpected source endpoint %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			os.WriteFile(path, []byte(fmt.Sprintf("sing:\n  api_key: score-test\n  base_url: %q\n", server.URL)), 0600)
			if _, err := config.Load(path); err != nil {
				t.Fatal(err)
			}
			c := &caller{sent: make(chan delivery, 10)}
			zero.APICallers.Store(987655, c)
			defer zero.APICallers.Delete(987655)
			ctx := zero.GetBot(987655)
			ctx.Event = &zero.Event{SelfID: 987655, GroupID: 123, UserID: 456, Message: message.Message{message.Text(zero.BotConfig.CommandPrefix + "score " + singer + "\n1 2\n天晴")}}
			ctx.State = zero.State{"command": "score"}
			handleScore(ctx)
			first := receive(t, c)
			if singer == "未知" || singer == "错误" {
				want := "没有找到这个歌手"
				if singer == "错误" {
					want = "乐谱内容第 2 行"
				}
				if !strings.Contains(first.segments[0].Data["text"], want) {
					t.Fatalf("wrong error: %+v", first)
				}
			} else {
				if first.segments[0].Type != "text" {
					t.Fatal("no warmup")
				}
				for i := 0; i < 2; i++ {
					got := receive(t, c)
					if got.target != 123 || got.segments[0].Type != "record" {
						t.Fatalf("bad delivery: %+v", got)
					}
				}
			}
			deadline := time.Now().Add(time.Second)
			for {
				if _, busy := pending.Load(destination{987655, 123, 456}); !busy {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("score pending not cleared")
				}
				time.Sleep(time.Millisecond)
			}
			if released != (singer == "" || singer == "洛") {
				t.Fatal("invalid score submitted")
			}
		})
	}
}

func TestScoreHelpAndSharedPending(t *testing.T) {
	c := &caller{sent: make(chan delivery, 10)}
	zero.APICallers.Store(987656, c)
	defer zero.APICallers.Delete(987656)
	ctx := zero.GetBot(987656)
	ctx.Event = &zero.Event{SelfID: 987656, UserID: 456, Message: message.Message{message.Text(zero.BotConfig.CommandPrefix + "score")}}
	handleScore(ctx)
	help := receive(t, c).segments
	if len(help) != 1 || help[0].Type != "image" || help[0].Data["file"] != message.ImageBytes(scoreHelpImage).Data["file"] {
		t.Fatal("score help must be a single embedded image")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("sing:\n  api_key: test\n"), 0600)
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	target := destination{987656, 0, 456}
	pending.Store(target, struct{}{})
	defer pending.Delete(target)
	ctx.Event.Message = message.Message{message.Text(zero.BotConfig.CommandPrefix + "score\n1\n天")}
	handleScore(ctx)
	if !strings.Contains(receive(t, c).segments[0].Data["text"], "上一首") {
		t.Fatal("did not share song guard")
	}
}

func TestScoreValidationHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		fmt.Fprint(w, `{"detail":[{"msg":"too long"}]}`)
	}))
	defer server.Close()
	var out any
	if err := newClient(server.URL, "key", "score").json(context.Background(), "POST", "/parse_score", nil, &out); err != httpError(422) {
		t.Fatal(err)
	}
}
