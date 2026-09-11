package omoi

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/config"
)

func TestSplitReplySendsSeparateMessages(t *testing.T) {
	cfg := config.OmoiConfig{SplitMarker: "<<<SPLIT>>>", SkipMarker: "[SKIP]", MaxMessageLen: 2000, TypingDelayMs: 60, MaxTypingDelaySec: 4}
	for _, tc := range []struct {
		name, reply string
		want        []string
	}{
		{"explicit markers", "第一句<<<SPLIT>>>第二句<<<SPLIT>>>第三句", []string{"第一句", "第二句", "第三句"}},
		{"missing markers", "第一句\n\n第二句\n\n第三句", []string{"第一句", "第二句", "第三句"}},
		{"windows and whitespace", " 第一段\r\n \t\r\n第二段\r\n\r\n\r\n 第三段 ", []string{"第一段", "第二段", "第三段"}},
		{"single newlines stay together", "第一行\n第二行", []string{"第一行\n第二行"}},
		{"explicit grouping wins", "第一条\n\n补充<<<SPLIT>>>第二条", []string{"第一条\n\n补充", "第二条"}},
		{"empty parts", "<<<SPLIT>>> 第一条 <<<SPLIT>>><<<SPLIT>>>第二条<<<SPLIT>>>", []string{"第一条", "第二条"}},
		{"skip", " [SKIP] ", nil},
		{"skip in reply", "前文\n\n[SKIP]", nil},
		{"empty", " \n\n ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []string
			var sleeps []time.Duration
			sendSplitReply(tc.reply, cfg, func(text string) { sent = append(sent, text) }, func(delay time.Duration) {
				if len(sent) == 0 {
					t.Error("delayed before the first message")
				}
				sleeps = append(sleeps, delay)
			})
			if !reflect.DeepEqual(sent, tc.want) {
				t.Fatalf("sent %q, want %q", sent, tc.want)
			}
			wantSleeps := len(tc.want) - 1
			if wantSleeps < 0 {
				wantSleeps = 0
			}
			if len(sleeps) != wantSleeps {
				t.Fatalf("got %d delays, want %d", len(sleeps), wantSleeps)
			}
		})
	}
}

func TestSSEChunkBoundariesDoNotBreakReplySplitting(t *testing.T) {
	stream := "event: delta\ndata: {\"content\":\"第一句<<<SP\"}\n\n" +
		"event: delta\ndata: {\"content\":\"LIT>>>第二句<<<SPLIT>>>第三句\"}\n\n" +
		"event: done\ndata: {}\n\n"
	reply, err := (&OmoiClient{}).consumeSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	sendSplitReply(reply, config.OmoiConfig{SplitMarker: "<<<SPLIT>>>", MaxMessageLen: 2000}, func(text string) { sent = append(sent, text) }, func(time.Duration) {})
	if !reflect.DeepEqual(sent, []string{"第一句", "第二句", "第三句"}) {
		t.Fatalf("sent %q", sent)
	}
}

func TestReplyFormatUsesConfiguredMarkers(t *testing.T) {
	prompt := withReplyFormat("用户的问题", config.OmoiConfig{SplitMarker: "[NEXT]", SkipMarker: "[QUIET]"})
	if !strings.HasPrefix(prompt, "用户的问题") || !strings.Contains(prompt, "[NEXT]") || !strings.Contains(prompt, "[QUIET]") || strings.Contains(prompt, "<<<SPLIT>>>") {
		t.Fatalf("unexpected format instruction: %s", prompt)
	}
}
