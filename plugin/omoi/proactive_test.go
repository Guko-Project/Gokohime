package omoi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestOmoiPolicyTrigger(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.TypingDelayMs = 0
	cfg.Omoi.MaxTypingDelaySec = 0
	cfg.Omoi.TriggerProbability = 0 // the legacy trigger must not matter
	db, err := database.InitWithDialect("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	clientOnce.Do(func() {})
	oldClient := client
	t.Cleanup(func() { client = oldClient })
	buffers.Delete(int64(321))
	t.Cleanup(func() { buffers.Delete(int64(321)) })

	var mu sync.Mutex
	policy := `{"configured":true,"enabled":true,"min_messages":2,"quiet_seconds":0}`
	var judge []string
	var puts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/agents/test":
			fmt.Fprint(w, `{"memory_config":{"enabled":false}}`)
		case "/sessions":
			fmt.Fprint(w, `{"id":"s-321"}`)
		case "/sessions/s-321/delivery":
			fmt.Fprint(w, `{"ok":true}`)
		case "/proactive/policy":
			q := r.URL.Query()
			if r.Method == http.MethodGet && (q.Get("agent_id") != "test" || q.Get("instance") != "qq:999" || q.Get("target") != "group:321") {
				t.Errorf("policy query %s", r.URL.RawQuery)
			}
			if r.Method == http.MethodPut {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				raw, _ := json.Marshal(body)
				puts = append(puts, string(raw))
				policy = `{"configured":true,"enabled":false,"overridden":true}`
			}
			fmt.Fprint(w, policy)
		case "/chat/messages":
			var body struct {
				ChannelContext struct {
					Proactive bool   `json:"proactive"`
					JudgeText string `json:"judge_text"`
				} `json:"channel_context"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !body.ChannelContext.Proactive {
				t.Error("unsolicited turn not marked proactive")
			}
			judge = append(judge, body.ChannelContext.JudgeText)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: delta\ndata: {\"content\":\"插话\"}\n\nevent: done\ndata: {}\n\n")
		default:
			t.Errorf("unexpected Omoi path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client = &OmoiClient{baseURL: server.URL, agentID: "test", httpClient: server.Client()}
	caller := recordingCaller{sent: make(chan string, 10)}
	zero.APICallers.Store(999, caller)
	t.Cleanup(func() { zero.APICallers.Delete(999) })
	ctx := zero.GetBot(999)
	ctx.Event = &zero.Event{SelfID: 999, UserID: 10, GroupID: 321, DetailType: "group", Sender: &zero.User{ID: 10, Card: "小明", Role: "member"}}
	ctx.State = zero.State{}

	say := func(text string) {
		ctx.Event.Message = message.Message{message.Text(text)}
		handleGroupObserve(ctx)
	}
	say("一起去吃饭吗")
	select {
	case sent := <-caller.sent:
		t.Fatalf("spoke before min_messages: %s", sent)
	case <-time.After(100 * time.Millisecond):
	}
	say("去哪家")
	select {
	case sent := <-caller.sent:
		if !strings.Contains(sent, "插话") {
			t.Fatalf("unexpected reply %s", sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("policy trigger did not speak")
	}
	mu.Lock()
	if len(judge) != 1 || judge[0] != "小明：一起去吃饭吗\n小明：去哪家" {
		t.Fatalf("judge text %q", judge)
	}
	mu.Unlock()

	// .active changes need a group admin; the change refreshes the policy.
	active := func(arg string) string {
		ctx.State = zero.State{"args": arg}
		handleActive(ctx)
		select {
		case sent := <-caller.sent:
			return sent
		case <-time.After(5 * time.Second):
			t.Fatal("no .active reply")
			return ""
		}
	}
	if got := active("off"); !strings.Contains(got, "群主或管理员") {
		t.Fatalf("member changed the policy: %s", got)
	}
	ctx.Event.Sender.Role = "admin"
	if got := active("off"); !strings.Contains(got, "主动发言：关闭") {
		t.Fatalf(".active off reply %s", got)
	}
	mu.Lock()
	if len(puts) != 1 || !strings.Contains(puts[0], `"config":{"enabled":false}`) {
		t.Fatalf("PUT bodies %q", puts)
	}
	mu.Unlock()
	ctx.State = zero.State{}
	say("a")
	say("b")
	select {
	case sent := <-caller.sent:
		t.Fatalf("disabled group spoke: %s", sent)
	case <-time.After(200 * time.Millisecond):
	}
}
