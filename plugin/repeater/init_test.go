package repeater

import (
	"reflect"
	"testing"

	"github.com/colanns/gokohime/internal/config"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestLoadRuntimeConfig(t *testing.T) {
	cfg := &config.Config{
		Bot: config.BotConfig{
			CommandPrefix: "!",
		},
		Repeater: config.RepeaterConfig{
			Groups:           []string{"123", "all"},
			MinMessageLength: 2,
			MinMessageTimes:  3,
			MaxRepeatTime:    4,
			Blacklist:        []string{"黑名单消息"},
		},
	}

	got := loadRuntimeConfig(cfg)
	if !got.allowAll {
		t.Fatalf("expected allowAll to be true")
	}
	if !got.isEnabledGroup(123) {
		t.Fatalf("expected group 123 to be enabled")
	}
	if !got.isBlacklisted("黑名单消息") {
		t.Fatalf("expected blacklist to contain message")
	}
	if got.minMessageLength != 2 || got.minMessageTimes != 3 || got.maxRepeatTime != 4 {
		t.Fatalf("unexpected repeater limits: %+v", got)
	}

	wantPrefixes := []string{".", "!"}
	if !reflect.DeepEqual(got.commandPrefixes, wantPrefixes) {
		t.Fatalf("unexpected command prefixes: got %v want %v", got.commandPrefixes, wantPrefixes)
	}
}

func TestLooksLikeCommandMessage(t *testing.T) {
	if !looksLikeCommandMessage(".kk", []string{"."}) {
		t.Fatalf("expected .kk to be recognized as command")
	}
	if looksLikeCommandMessage("普通消息", []string{"."}) {
		t.Fatalf("did not expect plain text to be recognized as command")
	}
	if looksLikeCommandMessage(".", []string{"."}) {
		t.Fatalf("did not expect bare prefix to be recognized as command")
	}
}

func TestBuildRepeaterMessage(t *testing.T) {
	src := message.Message{
		message.Reply("1"),
		message.Text("hello"),
		message.Segment{Type: "image", Data: map[string]string{"url": "https://example.com/a.png"}},
		message.Segment{Type: "face", Data: map[string]string{"id": "14"}},
		message.Segment{Type: "record", Data: map[string]string{"file": "voice.amr"}},
	}

	got := buildRepeaterMessage(src)
	if len(got) != 4 {
		t.Fatalf("unexpected segment count: got %d want 4", len(got))
	}
	if got[0].Type != "text" || got[1].Type != "image" || got[2].Type != "face" || got[3].Type != "record" {
		t.Fatalf("unexpected segment types: %#v", got)
	}
}

func TestIsRepeatCandidate(t *testing.T) {
	cfg := runtimeConfig{minMessageLength: 2}

	if cfg.isRepeatCandidate(message.Message{message.Text("a")}, "a") {
		t.Fatalf("single short text should not be repeatable")
	}
	if !cfg.isRepeatCandidate(message.Message{message.Text("ab")}, "ab") {
		t.Fatalf("text meeting minimum length should be repeatable")
	}
	if !cfg.isRepeatCandidate(message.Message{{Type: "image", Data: map[string]string{"url": "https://example.com/a.png"}}}, "") {
		t.Fatalf("image message should be repeatable regardless of text length")
	}
}
