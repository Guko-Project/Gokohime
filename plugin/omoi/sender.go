package omoi

import (
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// SendSplitReply handles SKIP detection, splitting by marker, and delayed sending.
func SendSplitReply(ctx *zero.Ctx, reply string) {
	cfg := config.Get().Omoi

	// Check for SKIP
	trimmed := strings.TrimSpace(reply)
	if trimmed == cfg.SkipMarker || strings.Contains(trimmed, cfg.SkipMarker) {
		return
	}

	// Split by marker
	parts := strings.Split(reply, cfg.SplitMarker)

	sent := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Delay between segments (not before first)
		if sent > 0 {
			delay := calcTypingDelay(part, cfg)
			time.Sleep(delay)
		}

		// Split further if over max length
		segments := splitLongText(part, cfg.MaxMessageLen)
		for j, seg := range segments {
			if j > 0 {
				time.Sleep(500 * time.Millisecond)
			}
			ctx.Send(message.Text(seg))
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
