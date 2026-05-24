package jrluck

import (
	"strconv"
	"testing"
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
