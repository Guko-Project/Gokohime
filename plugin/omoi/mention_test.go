package omoi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type mentionCaller struct {
	recordingCaller
	lookups int
}

func (c *mentionCaller) CallAPI(ctx context.Context, req zero.APIRequest) (zero.APIResponse, error) {
	if req.Action == "get_group_member_info" {
		c.lookups++
		switch req.Params["user_id"] {
		case int64(20):
			return zero.APIResponse{Data: gjson.Parse(`{"card":"群名片","nickname":"昵称"}`)}, nil
		case int64(30):
			return zero.APIResponse{Data: gjson.Parse(`{"nickname":"昵称"}`)}, nil
		default:
			return zero.APIResponse{RetCode: 100}, nil
		}
	}
	if req.Action == "send_private_msg" {
		return zero.APIResponse{Data: gjson.Parse(`{"message_id":1}`)}, nil
	}
	return c.recordingCaller.CallAPI(ctx, req)
}

func TestMessageTextMentions(t *testing.T) {
	caller := &mentionCaller{}
	zero.APICallers.Store(998, caller)
	t.Cleanup(func() { zero.APICallers.Delete(998) })
	ctx := zero.GetBot(998)
	ctx.Event = &zero.Event{SelfID: 998, UserID: 10, GroupID: 123}
	for _, tc := range []struct {
		name     string
		segments message.Message
		want     string
	}{
		{"logged failure", message.Message{message.At(998), message.Text("复述接下来的文字："), message.At(20)}, "复述接下来的文字：@群名片(20)"},
		{"order and fallback", message.Message{message.At(30), message.Text("问"), message.At(40), message.Text("\n你好")}, "@昵称(30)问@40\n你好"},
		{"mention only", message.Message{message.At(20)}, "@群名片(20)"},
		{"all", message.Message{message.AtAll()}, "@全体成员"},
		{"bare bot", message.Message{message.At(998), message.Text("  ")}, ""},
		{"literal and media", message.Message{message.Text("@手打名字"), message.Image("https://example.com/a")}, "@手打名字"},
		{"invalid", message.Message{message.Segment{Type: "at", Data: map[string]string{"qq": "bad"}}, message.Segment{Type: "at", Data: map[string]string{"qq": "0"}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx.Event.Message = tc.segments
			if got := messageText(ctx); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	caller.lookups = 0
	ctx.Event.Message = message.Message{message.At(20), message.At(20)}
	if got := messageText(ctx); got != "@群名片(20)@群名片(20)" || caller.lookups != 1 {
		t.Fatalf("duplicate lookup: %q, calls=%d", got, caller.lookups)
	}
	ctx.Event.GroupID = 0
	if got := messageText(ctx); got != "@20@20" {
		t.Fatalf("private fallback: %q", got)
	}
}

func TestMentionsReachOmoiAndMemory(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.TriggerProbability = 0
	db, err := database.Init(filepath.Join(t.TempDir(), "plugin.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var prompts []string
	var captured []memoryEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/agents/test":
			fmt.Fprint(w, `{"memory_config":{"enabled":true}}`)
		case r.URL.Path == "/sessions":
			fmt.Fprint(w, `{"id":"mention-session"}`)
		case strings.HasSuffix(r.URL.Path, "/delivery"):
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/memory-events"):
			var body struct {
				Events []memoryEvent `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			captured = append(captured, body.Events...)
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/chat/messages":
			var body struct {
				Content      []ContentBlock `json:"content"`
				MemoryEvents []memoryEvent  `json:"memory_events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, block := range body.Content {
				prompts = append(prompts, block.Text)
			}
			captured = append(captured, body.MemoryEvents...)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: delta\ndata: {\"content\":\"收到\"}\n\nevent: done\ndata: {}\n\n")
		default:
			t.Errorf("unexpected API %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	oldClient := client
	defer func() { client = oldClient }()
	clientOnce.Do(func() {})
	client = &OmoiClient{baseURL: server.URL, agentID: "test", httpClient: server.Client()}
	caller := &mentionCaller{recordingCaller: recordingCaller{sent: make(chan string, 10)}}
	zero.APICallers.Store(998, caller)
	t.Cleanup(func() { zero.APICallers.Delete(998); buffers.Delete(int64(321)) })
	ctx := zero.GetBot(998)
	ctx.Event = &zero.Event{SelfID: 998, UserID: 10, GroupID: 321, DetailType: "group", MessageID: int64(1), Message: message.Message{message.At(20)}}
	ctx.State = zero.State{}
	handleGroupObserve(ctx)
	if len(captured) != 1 || captured[0].Text != "@群名片(20)" {
		t.Fatalf("mention-only observation lost: %+v", captured)
	}
	ctx.Event.IsToMe = true
	ctx.Event.MessageID = int64(2)
	ctx.Event.Message = message.Message{message.At(998), message.Text("复述接下来的文字："), message.At(30)}
	handleGroupMention(ctx)
	if len(prompts) != 1 || !strings.Contains(prompts[0], "@群名片(20)") || !strings.Contains(prompts[0], "复述接下来的文字：@昵称(30)") {
		t.Fatalf("history or trigger mention lost: %q", prompts)
	}
	if len(captured) < 2 || captured[len(captured)-1].Text != "复述接下来的文字：@昵称(30)" {
		t.Fatalf("trigger memory lost: %+v", captured)
	}
	ctx.Event.Message = message.Message{message.At(20), message.Text(" .help")}
	handleGroupMention(ctx)
	if len(prompts) != 1 {
		t.Fatal("mention bypassed command filtering")
	}
	ctx.Event.GroupID = 0
	ctx.Event.DetailType = "private"
	ctx.Event.MessageID = int64(3)
	ctx.Event.Message = message.Message{message.At(20)}
	handlePrivateChat(ctx)
	if len(prompts) != 2 || !strings.Contains(prompts[1], "@20") {
		t.Fatalf("private mention lost: %q", prompts)
	}
}
