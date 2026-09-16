package omoi

import (
	"errors"
	"github.com/colanns/gokohime/internal/config"
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
		parts := deliveryParts(text, cfg)
		result := deliverParts(parts, progress, save, ok, func(string) bool { calls++; return true }, sleep)
		if result != "sent" || calls != 3 {
			t.Fatalf("state=%s calls=%d", result, calls)
		}
		if deliverParts(parts, progress, save, ok, func(string) bool { calls++; return true }, sleep) != "sent" || calls != 3 {
			t.Fatal("ack loss duplicated sends")
		}
	}
	calls := 0
	save := func(deliveryProgress) error { return nil }
	ok := func() error { return nil }
	sleep := func(time.Duration) error { return nil }
	send := func(string) bool { calls++; return true }
	parts := deliveryParts("one<<<SPLIT>>>two", cfg)
	if deliverParts(parts, deliveryProgress{Sent: 1, Sending: true}, save, ok, send, sleep) != "unknown" || calls != 0 {
		t.Fatal("uncertain send repeated")
	}
	if deliverParts(parts, deliveryProgress{Sent: 1}, save, ok, send, sleep) != "sent" || calls != 1 {
		t.Fatal("confirmed segment repeated")
	}
	if deliverParts(deliveryParts("[SKIP]", cfg), deliveryProgress{}, save, ok, send, sleep) != "skipped" || calls != 1 {
		t.Fatal("skip sent")
	}
	fail := func() error { return errors.New("cancelled") }
	if deliverParts(parts, deliveryProgress{}, save, fail, send, sleep) != "retry" || calls != 1 {
		t.Fatal("sent after lease rejected")
	}
}
