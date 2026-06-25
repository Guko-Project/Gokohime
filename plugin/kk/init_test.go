package kk

import "testing"

func TestParseKTVAddArgsFields(t *testing.T) {
	song, err := parseKTVAddArgs("群青 日 BV1xx411c7mD")
	if err != nil {
		t.Fatalf("parseKTVAddArgs failed: %v", err)
	}
	if song.Name != "群青" || song.Category != "日" || song.BV != "BV1xx411c7mD" {
		t.Fatalf("unexpected song: %+v", song)
	}
}

func TestParseKTVAddArgsMultiWordName(t *testing.T) {
	song, err := parseKTVAddArgs("Little Wish 粥批")
	if err != nil {
		t.Fatalf("parseKTVAddArgs failed: %v", err)
	}
	if song.Name != "Little Wish" || song.Category != "粥批" || song.BV != "" {
		t.Fatalf("unexpected song: %+v", song)
	}
}

func TestParseKTVAddArgsMultiWordNameWithBV(t *testing.T) {
	song, err := parseKTVAddArgs("Little Wish 粥批 BV1xx411c7mD")
	if err != nil {
		t.Fatalf("parseKTVAddArgs failed: %v", err)
	}
	if song.Name != "Little Wish" || song.Category != "粥批" || song.BV != "BV1xx411c7mD" {
		t.Fatalf("unexpected song: %+v", song)
	}
}

func TestParseKTVAddArgsPipes(t *testing.T) {
	song, err := parseKTVAddArgs("Little Wish | 粥批 | https://www.bilibili.com/video/BV1xx411c7mD")
	if err != nil {
		t.Fatalf("parseKTVAddArgs failed: %v", err)
	}
	if song.Name != "Little Wish" || song.Category != "粥批" || song.BV != "https://www.bilibili.com/video/BV1xx411c7mD" {
		t.Fatalf("unexpected song: %+v", song)
	}
}

func TestParseKTVAddArgsRequiresName(t *testing.T) {
	if _, err := parseKTVAddArgs("   "); err == nil {
		t.Fatalf("expected empty args to fail")
	}
}

func TestHashKTVSong(t *testing.T) {
	if hashKTVSong("群青") != hashKTVSong(" 群青 ") {
		t.Fatalf("expected hash to normalize surrounding spaces")
	}
}

func TestExactKTVCommandTextDoesNotMatchKKSCommands(t *testing.T) {
	if isExactKTVCommandText(".kksadd 日推 https://example.com", ".", "kk") {
		t.Fatalf("expected .kksadd not to match .kk")
	}
	if isExactKTVCommandText(".kks 日推", ".", "kk") {
		t.Fatalf("expected .kks not to match .kk")
	}
	if !isExactKTVCommandText(".kk 日推", ".", "kk") {
		t.Fatalf("expected .kk with args to match")
	}
	if !isExactKTVCommandText(".kk", ".", "kk") {
		t.Fatalf("expected exact .kk to match")
	}
}
