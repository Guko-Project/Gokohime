package tempban

import (
	"sync"
	"testing"
	"time"
)

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
