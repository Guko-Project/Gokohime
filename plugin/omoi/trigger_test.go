package omoi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("omoi: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestActiveTriggerGatesAndConcurrency(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.BufferSize = 3
	cfg.Omoi.TriggerCount = 2
	cfg.Omoi.TriggerIntervalSec = 180
	cfg.Omoi.TriggerProbability = 1
	cfg.Omoi.EnabledGroups = []int64{123}
	msg := BufferedMessage{Text: "hello"}
	b := &GroupBuffer{}
	if b.Push(msg, 123) || !b.Push(msg, 123) {
		t.Fatal("must trigger at the configured message count")
	}
	if b.Push(msg, 123) || b.Push(msg, 123) {
		t.Fatal("cooldown did not suppress messages")
	}
	b.lastSent = time.Now().Add(-181 * time.Second)
	if !b.Push(msg, 123) {
		t.Fatal("did not trigger after cooldown and enough new messages")
	}
	if b.count != 0 || len(b.Snapshot()) != 3 {
		t.Fatal("trigger did not reset count or buffer exceeded capacity")
	}
	snapshot := b.Snapshot()
	snapshot[0].Text = "changed"
	if b.Snapshot()[0].Text == "changed" {
		t.Fatal("snapshot aliases buffer")
	}
	for _, tc := range []struct {
		name        string
		probability float64
		group       int64
	}{
		{"disabled", 0, 123},
		{"other group", 1, 456},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Omoi.TriggerProbability = tc.probability
			buffer := &GroupBuffer{}
			for i := 0; i < 20; i++ {
				if buffer.Push(msg, tc.group) {
					t.Fatal("unexpected active trigger")
				}
			}
			if len(buffer.Snapshot()) != 3 {
				t.Fatal("disabled active chat must still retain context")
			}
		})
	}
	cfg.Omoi.TriggerProbability = 1
	cfg.Omoi.EnabledGroups = nil
	cfg.Omoi.TriggerCount = 1
	b = &GroupBuffer{}
	var triggered atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Push(msg, 456) {
				triggered.Add(1)
			}
		}()
	}
	wg.Wait()
	if triggered.Load() != 1 {
		t.Fatalf("concurrent messages triggered %d times, want 1", triggered.Load())
	}
}

type recordingCaller struct {
	sent chan string
}

func (c recordingCaller) CallAPI(_ context.Context, req zero.APIRequest) (zero.APIResponse, error) {
	switch req.Action {
	case "get_group_info":
		return zero.APIResponse{Data: gjson.Parse(`{"group_name":"测试群"}`)}, nil
	case "send_group_msg":
		data, _ := json.Marshal(req.Params["message"])
		c.sent <- string(data)
		return zero.APIResponse{Data: gjson.Parse(`{"message_id":1}`)}, nil
	default:
		return zero.APIResponse{}, fmt.Errorf("unexpected QQ API: %s", req.Action)
	}
}

func TestMentionAndActiveHandlers(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.TypingDelayMs = 0
	cfg.Omoi.MaxTypingDelaySec = 0
	db, err := database.InitWithDialect("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	clientOnce.Do(func() {})
	oldClient := client
	t.Cleanup(func() { client = oldClient })

	var requestCount atomic.Int32
	var mu sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/sessions/fresh-session/delivery", "/sessions/stale-session/delivery":
			fmt.Fprint(w, `{"ok":true}`)
		case "/sessions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"fresh-session"}`)
		case "/chat/messages":
			var body struct {
				SessionID string `json:"session_id"`
				Content   []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			requestCount.Add(1)
			if body.SessionID == "stale-session" {
				w.WriteHeader(404)
				return
			}
			prompt := body.Content[0].Text
			mu.Lock()
			prompts = append(prompts, prompt)
			mu.Unlock()
			reply := "收到"
			if strings.Contains(prompt, "只 @") {
				reply = "[SKIP]"
			}
			data, _ := json.Marshal(map[string]string{"content": reply})
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: delta\ndata: %s\n\nevent: done\ndata: {}\n\n", data)
		default:
			t.Errorf("unexpected Omoi path %s", req.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client = &OmoiClient{baseURL: server.URL, agentID: "test", httpClient: server.Client()}
	caller := recordingCaller{sent: make(chan string, 10)}
	zero.APICallers.Store(999, caller)
	t.Cleanup(func() { zero.APICallers.Delete(999) })
	ctx := zero.GetBot(999)
	ctx.Event = &zero.Event{SelfID: 999, UserID: 10, GroupID: 123, DetailType: "group", IsToMe: true}
	ctx.State = zero.State{}
	ctx.Event.Message = message.Message{message.At(999), message.Text("  ")}
	handleGroupMention(ctx)
	select {
	case sent := <-caller.sent:
		if !strings.Contains(sent, "我在，怎么啦") {
			t.Fatalf("bare mention did not acknowledge model SKIP: %s", sent)
		}
	default:
		t.Fatal("bare mention sent no QQ message")
	}
	if requestCount.Load() != 1 {
		t.Fatal("bare mention did not reach Omoi")
	}

	ctx.Event.Message = message.Message{message.Text(" .help")}
	handleGroupMention(ctx)
	if requestCount.Load() != 1 {
		t.Fatal("command was sent to Omoi")
	}

	// Simulate a deleted session and verify that active chat recovers it.
	cfg.Omoi.TriggerProbability = 1
	cfg.Omoi.TriggerCount = 2
	cfg.Omoi.SkipMarker = "[QUIET]"
	if err := database.SetPluginKV(context.Background(), nil, kvNamespace, "session:group:123", "stale-session"); err != nil {
		t.Fatal(err)
	}
	buffers.Delete(int64(123))
	t.Cleanup(func() { buffers.Delete(int64(123)) })
	ctx.Event.IsToMe = false
	ctx.Event.Message = message.Message{message.Text("一起去吃饭吗")}
	handleGroupObserve(ctx)
	if requestCount.Load() != 1 {
		t.Fatal("active handler ignored count threshold")
	}
	handleGroupObserve(ctx)
	select {
	case sent := <-caller.sent:
		if !strings.Contains(sent, "收到") {
			t.Fatalf("unexpected active reply: %s", sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active handler did not send after session recovery")
	}
	if requestCount.Load() != 3 {
		t.Fatalf("want bare mention + stale session + retry; got %d", requestCount.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || !strings.Contains(prompts[0], "只 @") ||
		!strings.Contains(prompts[1], "[QUIET]") || strings.Contains(prompts[1], "[SKIP]") {
		t.Fatalf("unexpected prompts: %q", prompts)
	}
}
