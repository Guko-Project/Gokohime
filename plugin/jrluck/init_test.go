package jrluck

import (
	"strconv"
	"testing"
	"time"
)

func TestLuckTemplateNumberAndImagePath(t *testing.T) {
	if got := luckTemplateNumber(0); got != 1 {
		t.Fatalf("luckTemplateNumber(0) = %d, want 1", got)
	}
	if got := luckImagePath(7); got != "data/jrrp/7.jpg" {
		t.Fatalf("luckImagePath(7) = %q", got)
	}
}

func TestComputeLuckIndexReroll(t *testing.T) {
	found := false
	for i := 1; i < 5000; i++ {
		qq := strconv.Itoa(i)
		base := computeLuckIndex(qq, func(int) bool { return false })
		if base == 0 {
			continue
		}

		rerollCheck := randomRP(strconv.Itoa(i + 50))
		if rerollCheck <= 50 {
			continue
		}

		got := computeLuckIndex(qq, func(int) bool { return true })
		want := randomRP(strconv.Itoa(i + 10086))
		if got != want {
			t.Fatalf("computeLuckIndex reroll = %d, want %d for qq=%s", got, want, qq)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("did not find a rerollable test case")
	}
}

func TestLuckDateInt(t *testing.T) {
	tests := []struct {
		name string
		at   string
		want int64
	}{
		{"before midnight UTC+8", "2026-09-11T15:59:59.999999999Z", 20260911},
		{"midnight UTC+8", "2026-09-11T16:00:00Z", 20260912},
		{"before old reset", "2026-09-11T23:59:59Z", 20260912},
		{"old reset stays same day", "2026-09-12T00:00:00Z", 20260912},
		{"end of same day", "2026-09-12T15:59:59Z", 20260912},
		{"next midnight", "2026-09-12T16:00:00Z", 20260913},
		{"year boundary", "2026-12-31T16:00:00Z", 20270101},
		{"leap day", "2028-02-28T16:00:00Z", 20280229},
		{"month boundary", "2028-02-29T16:00:00Z", 20280301},
		{"UTC+8 input", "2026-09-12T00:00:00+08:00", 20260912},
		{"different input timezone", "2026-09-11T09:00:00-07:00", 20260912},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339Nano, tt.at)
			if err != nil {
				t.Fatal(err)
			}
			if got := luckDateInt(at); got != tt.want {
				t.Fatalf("luckDateInt(%s) = %d, want %d", tt.at, got, tt.want)
			}
		})
	}
}
