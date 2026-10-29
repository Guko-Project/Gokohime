package tempban

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/commandutil"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestExactCommandRule(t *testing.T) {
	originalPrefix := zero.BotConfig.CommandPrefix
	t.Cleanup(func() { zero.BotConfig.CommandPrefix = originalPrefix })

	for _, prefix := range []string{".", "/", "!", "!!"} {
		zero.BotConfig.CommandPrefix = prefix
		for _, command := range []string{"ban", "unban"} {
			for _, tc := range []struct {
				suffix string
				want   bool
			}{
				{"", true},
				{" banana-pro 30", true},
				{"\tbanana-pro", true},
				{"\nbanana-pro", true},
				{"\u3000banana-pro", true},
				{"ana", false},
				{"ana-pro test", false},
				{"ner", false},
				{"-pro", false},
				{"_test", false},
			} {
				suffix := tc.suffix
				t.Run(prefix+command+suffix, func(t *testing.T) {
					ctx := &zero.Ctx{
						Event: &zero.Event{Message: message.Message{message.Text(prefix + command + suffix)}},
						State: zero.State{},
					}
					got := zero.CommandRule(command)(ctx) && exactCommandRule(command)(ctx)
					if got != tc.want {
						t.Fatalf("command match = %v, want %v", got, tc.want)
					}
					if got && (ctx.State["command"] != command || strings.TrimSpace(ctx.State["args"].(string)) != strings.TrimSpace(suffix)) {
						t.Fatalf("unexpected command state: %#v", ctx.State)
					}
				})
			}
		}
	}
}

func TestBanDoesNotInterceptBananaCommands(t *testing.T) {
	originalPrefix := zero.BotConfig.CommandPrefix
	t.Cleanup(func() { zero.BotConfig.CommandPrefix = originalPrefix })
	for _, prefix := range []string{".", "/"} {
		zero.BotConfig.CommandPrefix = prefix
		for _, command := range []string{"banana", "banana-pro"} {
			t.Run(prefix+command, func(t *testing.T) {
				ctx := &zero.Ctx{
					Event: &zero.Event{Message: message.Message{message.Text(prefix + command + " 梦幻森林")}},
					State: zero.State{},
				}
				if zero.CommandRule("ban")(ctx) && exactCommandRule("ban")(ctx) {
					t.Fatal("banana command was intercepted by ban")
				}
				if !commandutil.ReplyableCommandRule("banana-pro", "banana")(ctx) {
					t.Fatal("banana command did not reach its own matcher")
				}
				if ctx.State["command"] != command || ctx.State["args"] != "梦幻森林" {
					t.Fatalf("unexpected banana command state: %#v", ctx.State)
				}
			})
		}
	}
}

func TestNormalizeCommandName(t *testing.T) {
	cases := map[string]string{
		".KK":   "kk",
		"/cp":   "cp",
		"!啃":    "啃",
		"  ai ": "ai",
	}

	for input, want := range cases {
		if got := normalizeCommandName(input); got != want {
			t.Fatalf("normalizeCommandName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExtractCommandName(t *testing.T) {
	cmd, ok := extractCommandName(".ban .kk 30", ".")
	if !ok || cmd != "ban" {
		t.Fatalf("expected ban command, got %q ok=%v", cmd, ok)
	}

	cmd, ok = extractCommandName("普通消息", ".")
	if ok || cmd != "" {
		t.Fatalf("expected plain text not to be treated as command")
	}
}

func TestIsBannedNormalizesAndExpires(t *testing.T) {
	bannedCommands = sync.Map{}

	bannedCommands.Store(banKey(100, ".kk"), time.Now().Add(time.Minute))
	if !IsBanned(100, "kk") {
		t.Fatalf("expected kk to be banned")
	}

	expiredKey := banKey(100, "cp")
	bannedCommands.Store(expiredKey, time.Now().Add(-time.Minute))
	if IsBanned(100, ".cp") {
		t.Fatalf("expected expired cp ban to be cleared")
	}
	if _, ok := bannedCommands.Load(expiredKey); ok {
		t.Fatalf("expected expired ban entry to be deleted")
	}
}
