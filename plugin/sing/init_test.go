package sing

import (
	"context"
	"encoding/base64"
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
	for _, groupID := range []int64{123, 0} {
		t.Run(fmt.Sprint(groupID), func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/search":
					<-release
					fmt.Fprint(w, `[{"id":"1","name":"雨爱","artists":["杨丞琳"]}]`)
				case "/release_task":
					fmt.Fprint(w, `{"task_id":"task"}`)
				case "/query_result":
					fmt.Fprint(w, `{"status":"done","outputs":{"score":"/audio"}}`)
				case "/v1/audio":
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
			ctx.State = zero.State{"args": " 雨爱 "}
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
			close(release)
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

func TestCommandBoundary(t *testing.T) {
	for _, tc := range []struct {
		suffix string
		want   bool
	}{{"", true}, {" 雨爱", true}, {"\t雨爱", true}, {"ing", false}, {"er", false}} {
		ctx := &zero.Ctx{Event: &zero.Event{Message: message.Message{message.Text(zero.BotConfig.CommandPrefix + "sing" + tc.suffix)}}}
		if commandBoundary(ctx) != tc.want {
			t.Errorf("boundary %q", tc.suffix)
		}
	}
}
