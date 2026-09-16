package omoi

import (
	"math/rand"
	"slices"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
)

// BufferedMessage holds one message in the ring buffer.
type BufferedMessage struct {
	MessageID    string
	ReplyToID    string
	OriginalText string
	UserID       int64
	Nickname     string
	Text         string
	Media        []MediaReference
	Time         time.Time
}

// Append records mentions too: their blocking handler prevents the observer from running.
func (b *GroupBuffer) Append(msg BufferedMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, msg)
	if max := config.Get().Omoi.BufferSize; max > 0 && len(b.messages) > max {
		b.messages = b.messages[len(b.messages)-max:]
	}
}

// GroupBuffer is a per-group ring buffer with trigger state.
type GroupBuffer struct {
	mu       sync.Mutex
	messages []BufferedMessage
	count    int       // messages since last trigger
	lastSent time.Time // last active trigger time
}

var buffers sync.Map // map[int64]*GroupBuffer

// getBuffer returns the buffer for a group, creating one if needed.
func getBuffer(groupID int64) *GroupBuffer {
	val, _ := buffers.LoadOrStore(groupID, &GroupBuffer{})
	return val.(*GroupBuffer)
}

// Push adds a message to the group buffer and returns whether a trigger should fire.
func (b *GroupBuffer) Push(msg BufferedMessage, groupID int64) bool {
	cfg := config.Get().Omoi

	b.mu.Lock()
	defer b.mu.Unlock()

	// Add to ring buffer
	b.messages = append(b.messages, msg)
	if len(b.messages) > cfg.BufferSize {
		b.messages = b.messages[len(b.messages)-cfg.BufferSize:]
	}
	b.count++

	// Decide and reserve the trigger under the same lock so concurrent messages cannot fire twice.
	if len(cfg.EnabledGroups) > 0 && !slices.Contains(cfg.EnabledGroups, groupID) {
		return false
	}

	// Check trigger conditions
	if cfg.TriggerProbability <= 0 {
		return false
	}
	if b.count < cfg.TriggerCount {
		return false
	}
	if time.Since(b.lastSent) < time.Duration(cfg.TriggerIntervalSec)*time.Second {
		return false
	}

	if rand.Float64() >= cfg.TriggerProbability {
		return false
	}
	b.count = 0
	b.lastSent = time.Now()
	return true
}

// Snapshot returns a copy of the current buffered messages.
func (b *GroupBuffer) Snapshot() []BufferedMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]BufferedMessage, len(b.messages))
	copy(cp, b.messages)
	return cp
}
