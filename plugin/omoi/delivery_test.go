package omoi

import (
	"encoding/json"
	"errors"
	"github.com/colanns/gokohime/internal/config"
	"strings"
	"testing"
	"time"
)

func TestScheduledDeliverySegmentsAndRecovery(t *testing.T) {
	cfg := config.OmoiConfig{SplitMarker: "<<<SPLIT>>>", SkipMarker: "[SKIP]", MaxMessageLen: 2000, MaxTypingDelaySec: 1}
	for _, text := range []string{"one<<<SPLIT>>>two<<<SPLIT>>>three", "one\r\n\r\ntwo\r\n\r\nthree"} {
		progress := deliveryProgress{}
		calls := 0
		save := func(v deliveryProgress) error { progress = v; return nil }
		ok := func() error { return nil }
		sleep := func(time.Duration) error { return nil }
		parts := deliveryParts(scheduledDelivery{Text: text}, cfg)
		result := deliverParts(parts, progress, save, ok, func(deliveryPart) bool { calls++; return true }, sleep)
		if result != "sent" || calls != 3 {
			t.Fatalf("state=%s calls=%d", result, calls)
		}
		if deliverParts(parts, progress, save, ok, func(deliveryPart) bool { calls++; return true }, sleep) != "sent" || calls != 3 {
			t.Fatal("ack loss duplicated sends")
		}
	}
	calls := 0
	save := func(deliveryProgress) error { return nil }
	ok := func() error { return nil }
	sleep := func(time.Duration) error { return nil }
	send := func(deliveryPart) bool { calls++; return true }
	parts := deliveryParts(scheduledDelivery{Text: "one<<<SPLIT>>>two"}, cfg)
	if deliverParts(parts, deliveryProgress{Sent: 1, Sending: true}, save, ok, send, sleep) != "unknown" || calls != 0 {
		t.Fatal("uncertain send repeated")
	}
	if deliverParts(parts, deliveryProgress{Sent: 1}, save, ok, send, sleep) != "sent" || calls != 1 {
		t.Fatal("confirmed segment repeated")
	}
	if deliverParts(deliveryParts(scheduledDelivery{Text: "[SKIP]"}, cfg), deliveryProgress{}, save, ok, send, sleep) != "skipped" || calls != 1 {
		t.Fatal("skip sent")
	}
	fail := func() error { return errors.New("cancelled") }
	if deliverParts(parts, deliveryProgress{}, save, fail, send, sleep) != "retry" || calls != 1 {
		t.Fatal("sent after lease rejected")
	}
}

func TestScheduledDeliveryWithImages(t *testing.T) {
	cfg := config.OmoiConfig{SplitMarker: "<<<SPLIT>>>", SkipMarker: "[SKIP]", MaxMessageLen: 2000, MaxTypingDelaySec: 1}
	d := scheduledDelivery{Text: "早安<<<SPLIT>>>吃了吗", Content: []ContentBlock{{Type: "image", FileID: "f1", MediaType: "image/gif"}, {Type: "text", Text: "早安<<<SPLIT>>>吃了吗"}}}
	parts := deliveryParts(d, cfg)
	if len(parts) != 3 || parts[0].FileID != "f1" || parts[1].Text != "早安" || parts[2].Text != "吃了吗" || parts[1].Delay == 0 {
		t.Fatalf("parts: %+v", parts)
	}
	// Progress is persisted as JSON; image parts must survive a restart.
	raw, _ := json.Marshal(deliveryProgress{Parts: parts})
	var back deliveryProgress
	if json.Unmarshal(raw, &back) != nil || back.Parts[0].FileID != "f1" {
		t.Fatalf("progress lost image: %s", raw)
	}
	var sent []string
	save := func(deliveryProgress) error { return nil }
	ok := func() error { return nil }
	sleep := func(time.Duration) error { return nil }
	state := deliverParts(parts, deliveryProgress{}, save, ok, func(p deliveryPart) bool { sent = append(sent, p.FileID+p.Text); return true }, sleep)
	if state != "sent" || strings.Join(sent, "|") != "f1|早安|吃了吗" {
		t.Fatalf("state=%s sent=%v", state, sent)
	}
}
