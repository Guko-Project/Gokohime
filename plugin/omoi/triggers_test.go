package omoi

import (
	"strings"
	"testing"
	"time"
)

func TestObserveTriggers(t *testing.T) {
	future := time.Now().Add(time.Hour)
	p := &proactivePolicy{Configured: true, Enabled: true, Triggers: []proactiveTrigger{
		{ID: "name", MinMessages: 1, BypassLimits: true, Keywords: []string{"GZJ"}},
		{ID: "chatter", MinMessages: 3},
	}}
	b := &GroupBuffer{}
	say := func(text string) (string, bool) {
		return b.observe(BufferedMessage{Nickname: "小明", Text: text}, p, func(string) { t.Fatal("no quiet timer expected") })
	}
	if id, ok := say("hello"); ok {
		t.Fatalf("fired %q on the first message", id)
	}
	if id, ok := say("gzj 在吗"); !ok || id != "name" {
		t.Fatalf("keyword trigger: %q %v", id, ok)
	}
	b.firing = false
	// The keyword trigger reset; chatter kept counting and fires on its third message.
	if id, ok := say("third"); !ok || id != "chatter" {
		t.Fatalf("chatter trigger: %q %v", id, ok)
	}
	b.firing = false

	// During cooldown only triggers that bypass the limits may ask.
	p.NextAllowedAt = &future
	for range 3 {
		if id, ok := say("chat"); ok {
			t.Fatalf("%q fired during cooldown", id)
		}
	}
	if id, ok := say("叫一下 gzj"); !ok || id != "name" {
		t.Fatalf("bypass trigger during cooldown: %q %v", id, ok)
	}
	b.firing = false

	// Older Omoi versions: one trigger without an id from the top level.
	b = &GroupBuffer{}
	legacy := &proactivePolicy{Configured: true, Enabled: true, MinMessages: 2}
	if _, ok := b.observe(BufferedMessage{Text: "a"}, legacy, nil); ok {
		t.Fatal("legacy fired early")
	}
	if id, ok := b.observe(BufferedMessage{Text: "b"}, legacy, nil); !ok || id != "" {
		t.Fatalf("legacy trigger: %q %v", id, ok)
	}
}

func TestQuietTriggerFiresWithItsID(t *testing.T) {
	p := &proactivePolicy{Configured: true, Enabled: true, Triggers: []proactiveTrigger{{ID: "q", MinMessages: 1, QuietSeconds: 1}}}
	b := &GroupBuffer{}
	got := make(chan string, 1)
	if _, ok := b.observe(BufferedMessage{Text: "x"}, p, func(id string) { got <- id }); ok {
		t.Fatal("quiet trigger fired immediately")
	}
	select {
	case id := <-got:
		if id != "q" {
			t.Fatalf("fired %q", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quiet trigger never fired")
	}
}

func TestDescribeTriggers(t *testing.T) {
	var p proactivePolicy
	p.Configured, p.Enabled = true, true
	p.Triggers = []proactiveTrigger{{ID: "name", MinMessages: 1, BypassLimits: true}, {ID: "chatter", MinMessages: 5, QuietSeconds: 20}}
	chains := []policyChains{
		{ID: "name", Label: "被叫到", Stages: []policyStage{{Type: "keyword", Patterns: []string{"鸽子", "gzj"}}}},
		{ID: "chatter", Stages: []policyStage{{Type: "probability", Probability: 0.3}}},
	}
	p.Config.Triggers = &chains
	got := describePolicy(&p)
	for _, want := range []string{"触发器（任一通过即发言）", "1. 被叫到：关键词「鸽子/gzj」（不受冷却限制）", "2. chatter：概率 30%（5 条新消息，安静 20 秒）"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	if got := describePolicy(&proactivePolicy{Configured: true, OutOfScope: true}); !strings.Contains(got, "生效范围") {
		t.Fatalf("out of scope: %s", got)
	}
}
