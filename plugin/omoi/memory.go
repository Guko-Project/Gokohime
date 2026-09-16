package omoi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type sourceContext struct {
	Kind           string `json:"kind"`
	ConnectorID    string `json:"connector_id"`
	ConversationID string `json:"conversation_id"`
}

type memoryEvent struct {
	ExternalID string    `json:"external_id"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Text       string    `json:"text"`
	ReplyToID  string    `json:"reply_to_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type memoryContextKey struct{}
type memoryContext struct {
	Source sourceContext
	Events []memoryEvent
}

// The agent owns the only memory switch. Cache silent-observer checks briefly;
// the server rechecks the live setting on every ingestion and extraction.
func (c *OmoiClient) memoryEnabled(ctx context.Context) bool {
	c.memoryMu.Lock()
	defer c.memoryMu.Unlock()
	if time.Now().Before(c.memoryCheckAt) {
		return c.memoryActive
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var agent struct {
		MemoryConfig struct {
			Enabled bool `json:"enabled"`
		} `json:"memory_config"`
	}
	err := c.deliveryAPI(ctx, http.MethodGet, "/agents/"+c.agentID, nil, &agent)
	c.memoryActive = err == nil && agent.MemoryConfig.Enabled
	c.memoryCheckAt = time.Now().Add(30 * time.Second)
	return c.memoryActive
}

func withMemory(ctx context.Context, qq *zero.Ctx, messages []BufferedMessage) context.Context {
	if qq.Event.SelfID <= 0 || qq.Event.UserID <= 0 || qq.Event.GroupID < 0 {
		return ctx
	}
	if client == nil || !client.memoryEnabled(ctx) {
		return ctx
	}
	src := sourceContext{Kind: "qq_private", ConnectorID: strconv.FormatInt(qq.Event.SelfID, 10), ConversationID: strconv.FormatInt(qq.Event.UserID, 10)}
	if qq.Event.GroupID != 0 {
		src.Kind = "qq_group"
		src.ConversationID = strconv.FormatInt(qq.Event.GroupID, 10)
	}
	inputs := make([]BufferedMessage, 0, len(messages))
	for _, m := range messages {
		if m.UserID != qq.Event.SelfID {
			inputs = append(inputs, m)
		}
	}
	return context.WithValue(ctx, memoryContextKey{}, memoryContext{Source: src, Events: memoryEvents(inputs)})
}

func memoryFrom(ctx context.Context) (memoryContext, bool) {
	v, ok := ctx.Value(memoryContextKey{}).(memoryContext)
	return v, ok
}

func originalMessage(qq *zero.Ctx, text string) BufferedMessage {
	stamp := time.Unix(qq.Event.Time, 0)
	if qq.Event.Time <= 0 {
		stamp = time.Now()
	}
	id := ""
	if qq.Event.MessageID != nil {
		id = fmt.Sprint(qq.Event.MessageID)
	}
	reply := ""
	for _, seg := range qq.Event.Message {
		if seg.Type == "reply" {
			reply = seg.Data["id"]
			break
		}
	}
	return BufferedMessage{MessageID: id, ReplyToID: reply, OriginalText: text, Text: text, UserID: qq.Event.UserID, Nickname: getNickname(qq), Time: stamp}
}

func memoryEvents(messages []BufferedMessage) []memoryEvent {
	events := []memoryEvent{}
	seen := map[string]bool{}
	budget := 24000
	// Keep the trigger/newest events if a long snapshot exceeds the ingestion limit.
	for i := len(messages) - 1; i >= 0 && len(events) < 32; i-- {
		m := messages[i]
		n := utf8.RuneCountInString(m.OriginalText)
		if m.MessageID == "" || seen[m.MessageID] || m.UserID <= 0 || strings.TrimSpace(m.OriginalText) == "" || n > 4000 || n > budget {
			continue
		}
		name := []rune(m.Nickname)
		if len(name) > 100 {
			name = name[:100]
		}
		events = append(events, memoryEvent{ExternalID: m.MessageID, AuthorID: strconv.FormatInt(m.UserID, 10), AuthorName: string(name), Text: m.OriginalText, ReplyToID: m.ReplyToID, OccurredAt: m.Time})
		seen[m.MessageID] = true
		budget -= n
	}
	slices.Reverse(events)
	return events
}

func (c *OmoiClient) IngestMemory(ctx context.Context, sessionID string, events []memoryEvent) error {
	if len(events) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sessions/"+sessionID+"/memory-events", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("memory ingestion: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Capture is independent of the probabilistic reply trigger. The server deduplicates
// event IDs, so a later mention may safely resend the same buffered evidence.
func captureObservedMemory(qq *zero.Ctx, msg BufferedMessage) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(withMemory(requestContext(), qq, []BufferedMessage{msg}), 5*time.Second)
	defer cancel()
	if !client.memoryEnabled(ctx) {
		return
	}
	m, _ := memoryFrom(ctx)
	if len(m.Events) == 0 {
		return
	}
	id, err := getGroupSession(ctx, qq.Event.GroupID, strconv.FormatInt(qq.Event.GroupID, 10))
	if err == nil {
		err = client.IngestMemory(ctx, id, m.Events)
	}
	if err != nil && strings.Contains(err.Error(), "HTTP 404") {
		id, err = getOrCreateSessionAfterFailure(ctx, "session:group:"+strconv.FormatInt(qq.Event.GroupID, 10), "群:"+strconv.FormatInt(qq.Event.GroupID, 10), id)
		if err == nil {
			err = client.IngestMemory(ctx, id, m.Events)
		}
	}
	if err != nil {
		log.Warnf("[omoi] memory capture failed for group %d (buffered events can be retried with the next conversation)", qq.Event.GroupID)
	}
}
