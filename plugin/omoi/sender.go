package omoi

import (
	"math/rand"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var replyParagraphBreak = regexp.MustCompile(`\n[\t ]*\n`)

// SendSplitReply sends one QQ message per reply part, with typing delays.
func SendSplitReply(ctx *zero.Ctx, reply string) {
	sendSplitReply(reply, config.Get().Omoi, func(text string) {
		ctx.Send(message.Text(text))
	}, time.Sleep)
}

func sendSplitReply(reply string, cfg config.OmoiConfig, send func(string), sleep func(time.Duration)) {

	// Check for SKIP
	trimmed := strings.TrimSpace(reply)
	if cfg.SkipMarker != "" && strings.Contains(trimmed, cfg.SkipMarker) {
		return
	}

	// Explicit markers take priority. Some models return blank paragraphs instead.
	var parts []string
	if cfg.SplitMarker != "" && strings.Contains(reply, cfg.SplitMarker) {
		parts = strings.Split(reply, cfg.SplitMarker)
	} else {
		parts = replyParagraphBreak.Split(strings.ReplaceAll(reply, "\r\n", "\n"), -1)
	}

	sent := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Delay between segments (not before first)
		if sent > 0 {
			delay := calcTypingDelay(part, cfg)
			sleep(delay)
		}

		// Split further if over max length
		segments := splitLongText(part, cfg.MaxMessageLen)
		for j, seg := range segments {
			if j > 0 {
				sleep(500 * time.Millisecond)
			}
			send(seg)
		}
		sent++
	}
}

// calcTypingDelay simulates human typing delay based on character count.
func calcTypingDelay(text string, cfg config.OmoiConfig) time.Duration {
	charCount := utf8.RuneCountInString(text)
	baseMs := cfg.TypingDelayMs * charCount

	// Add random jitter ±30%
	jitter := float64(baseMs) * 0.3
	actualMs := float64(baseMs) + (rand.Float64()*2-1)*jitter

	d := time.Duration(actualMs) * time.Millisecond

	// Clamp
	minDelay := 1 * time.Second
	maxDelay := time.Duration(cfg.MaxTypingDelaySec) * time.Second
	if d < minDelay {
		d = minDelay
	}
	if d > maxDelay {
		d = maxDelay
	}
	return d
}

// splitLongText splits text by paragraphs if it exceeds maxLen.
func splitLongText(text string, maxLen int) []string {
	if utf8.RuneCountInString(text) <= maxLen {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var result []string
	var current strings.Builder

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if current.Len() > 0 && utf8.RuneCountInString(current.String())+utf8.RuneCountInString(p)+2 > maxLen {
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(p)
	}
	if current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}

	if len(result) == 0 {
		result = []string{text}
	}
	return result
}
