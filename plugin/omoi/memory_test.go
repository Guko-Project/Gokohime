package omoi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestMemoryEventsKeepOriginalSpeakers(t *testing.T) {
	stamp := time.Now()
	messages := []BufferedMessage{
		{MessageID: "1", UserID: 111, Nickname: "same name", OriginalText: "我喜欢茶", Text: "flattened prompt", Time: stamp},
		{MessageID: "2", UserID: 222, Nickname: "same name", OriginalText: "我喜欢咖啡", ReplyToID: "1", Time: stamp},
		{MessageID: "1", UserID: 111, Nickname: "same name", OriginalText: "我喜欢茶", Time: stamp},
		{MessageID: "3", UserID: 333, Text: "[unavailable image]", Time: stamp},
	}
	events := memoryEvents(messages)
	if len(events) != 2 {
		t.Fatalf("expected two unique raw messages: %+v", events)
	}
	for _, e := range events {
		if e.ExternalID == "1" && (e.AuthorID != "111" || e.Text != "我喜欢茶") {
			t.Fatalf("lost identity/evidence: %+v", e)
		}
	}
	if strings.Contains(fmt.Sprint(events), "flattened prompt") {
		t.Fatal("captured synthetic prompt")
	}
}

func TestMemoryObservationDoesNotNeedAReplyAndSessionCreationIsAtomic(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.EnabledGroups = []int64{123}
	cfg.Omoi.TriggerProbability = 0
	db, err := database.Init(filepath.Join(t.TempDir(), "plugin.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var created, ingested, checks atomic.Int32
	var enabled atomic.Bool
	enabled.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agents/memory-agent":
			checks.Add(1)
			fmt.Fprintf(w, `{"memory_config":{"enabled":%t}}`, enabled.Load())
		case "/sessions":
			created.Add(1)
			var body struct {
				Source sourceContext `json:"source_context"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Source.Kind != "qq_group" || body.Source.ConnectorID != "99" || (body.Source.ConversationID != "123" && body.Source.ConversationID != "456") {
				t.Errorf("missing group binding: %+v", body)
			}
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"id":"bound-session-%s"}`, body.Source.ConversationID)
		case "/sessions/bound-session-123/memory-events", "/sessions/bound-session-456/memory-events":
			ingested.Add(1)
			var body struct {
				Events []memoryEvent `json:"events"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Events) != 1 || body.Events[0].AuthorID != "111" || body.Events[0].ExternalID != "42" || body.Events[0].Text != "我喜欢茶" {
				t.Errorf("bad evidence: %+v", body)
			}
			w.WriteHeader(202)
		default:
			t.Errorf("unexpected reply call %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	oldClient := client
	defer func() { client = oldClient }()
	client = &OmoiClient{baseURL: server.URL, agentID: "memory-agent", httpClient: server.Client()}
	clientOnce = sync.Once{}
	clientOnce.Do(func() {})
	if err := database.SetPluginKV(context.Background(), nil, kvNamespace, "session:group:123", "legacy-unbound"); err != nil {
		t.Fatal(err)
	}
	zero.APICallers.Store(99, recordingCaller{sent: make(chan string, 10)})
	defer zero.APICallers.Delete(99)
	qq := zero.GetBot(99)
	qq.Event = &zero.Event{SelfID: 99, UserID: 111, GroupID: 123, MessageID: int64(42), Time: time.Now().Unix(), Message: message.Message{message.Text("我喜欢茶")}}
	qq.State = zero.State{}
	ctx := withMemory(context.Background(), qq, nil)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, err := getGroupSession(ctx, 123, "test")
			if err != nil || id != "bound-session-123" {
				t.Errorf("session: %s %v", id, err)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("created %d sessions for one group", created.Load())
	}
	defer buffers.Delete(int64(123))
	handleGroupObserve(qq)
	if ingested.Load() != 1 {
		t.Fatal("non-triggering observation was not captured")
	}
	qq.Event.GroupID = 456
	handleGroupObserve(qq)
	defer buffers.Delete(int64(456))
	if ingested.Load() != 2 || checks.Load() != 1 {
		t.Fatal("agent memory must capture all groups and cache its switch")
	}
	enabled.Store(false)
	client.memoryCheckAt = time.Time{}
	handleGroupObserve(qq)
	if ingested.Load() != 2 {
		t.Fatal("disabled agent memory captured an event")
	}
}

func TestConcurrentSplitRepliesStayContiguous(t *testing.T) {
	cfg := config.OmoiConfig{SplitMarker: "<<<SPLIT>>>", MaxMessageLen: 2000}
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var sent []string
	var wg sync.WaitGroup
	send := func(text string) { mu.Lock(); sent = append(sent, text); mu.Unlock() }
	wg.Add(1)
	go func() {
		defer wg.Done()
		withReplyLock("group-test", func() {
			sendSplitReply("A1<<<SPLIT>>>A2", cfg, send, func(time.Duration) { close(started); <-release })
		})
	}()
	<-started
	wg.Add(1)
	go func() {
		defer wg.Done()
		withReplyLock("group-test", func() { sendSplitReply("B1<<<SPLIT>>>B2", cfg, send, func(time.Duration) {}) })
	}()
	close(release)
	wg.Wait()
	if !reflect.DeepEqual(sent, []string{"A1", "A2", "B1", "B2"}) {
		t.Fatalf("interleaved replies: %v", sent)
	}
}

func TestMemorySessionsKeepScheduledDeliveryAndResetBothMappings(t *testing.T) {
	cfg := testConfig(t)
	cfg.Omoi.SplitMarker = "<<<SPLIT>>>"
	cfg.Omoi.MaxTypingDelaySec = 0
	db, err := database.Init(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var revoked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			revoked = append(revoked, r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	old := client
	defer func() { client = old }()
	client = &OmoiClient{baseURL: server.URL, agentID: "memory-agent", httpClient: server.Client()}
	ctx := context.Background()
	for key, session := range map[string]string{"session:group:123": "legacy", "session:group:123:memory:memory-agent:99": "bound"} {
		if err := database.SetPluginKV(ctx, nil, kvNamespace, key, session); err != nil {
			t.Fatal(err)
		}
	}
	sent := make(chan string, 10)
	zero.APICallers.Store(99, recordingCaller{sent: sent})
	defer zero.APICallers.Delete(99)
	bot := zero.GetBot(99)
	for _, session := range []string{"legacy", "bound"} {
		client.deliver(ctx, bot, scheduledDelivery{ID: session, SessionID: session, Instance: "qq:99", Channel: "qq", Target: "group:123", Text: "one<<<SPLIT>>>two"})
	}
	if len(sent) != 4 {
		t.Fatalf("scheduled split sends = %d, want 4", len(sent))
	}
	ctx = channelRequest(ctx, &zero.Event{SelfID: 99, UserID: 111, GroupID: 123}, false)
	if err := resetGroupSession(ctx, 123); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(revoked, []string{"/sessions/legacy/delivery", "/sessions/bound/delivery"}) {
		t.Fatalf("reset did not cancel both mappings: %v", revoked)
	}
	client.deliver(ctx, bot, scheduledDelivery{ID: "after-reset", SessionID: "bound", Instance: "qq:99", Channel: "qq", Target: "group:123", Text: "must not send"})
	if len(sent) != 4 {
		t.Fatal("scheduled task sent after reset")
	}
}
