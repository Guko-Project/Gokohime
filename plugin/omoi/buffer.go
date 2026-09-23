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
	seq          uint64 // per-group monotonic, assigned on buffer entry
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
// It returns the sequence number assigned to the buffered copy.
func (b *GroupBuffer) Append(msg BufferedMessage) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSeq++
	msg.seq = b.nextSeq
	b.messages = append(b.messages, msg)
	if max := config.Get().Omoi.BufferSize; max > 0 && len(b.messages) > max {
		b.messages = b.messages[len(b.messages)-max:]
	}
	return msg.seq
}

// GroupBuffer is a per-group ring buffer with trigger state.
type GroupBuffer struct {
	mu       sync.Mutex
	messages []BufferedMessage
	nextSeq  uint64       // last assigned seq
	sentSeq  uint64       // highest seq durably delivered to the Omoi session
	sendMu   sync.Mutex   // serializes pending+send+markSent so deltas never overlap
	count    int          // messages since last trigger
	lastSent time.Time    // last active trigger time
}

// pending returns the messages not yet delivered to the session. Seed mode
// returns the whole buffer for a fresh session. omitted counts messages that
// left the buffer before they could be sent.
func (b *GroupBuffer) pending(seed bool) (msgs []BufferedMessage, omitted int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	start := 0
	if !seed {
		for start < len(b.messages) && b.messages[start].seq <= b.sentSeq {
			start++
		}
		if start < len(b.messages) && b.messages[start].seq > b.sentSeq+1 {
			omitted = int(b.messages[start].seq - b.sentSeq - 1)
		}
	}
	return append([]BufferedMessage(nil), b.messages[start:]...), omitted
}

// markSent advances the delivery cursor after a successful send.
func (b *GroupBuffer) markSent(seq uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if seq > b.sentSeq {
		b.sentSeq = seq
	}
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
	b.nextSeq++
	msg.seq = b.nextSeq
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
