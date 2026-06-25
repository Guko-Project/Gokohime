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

func TestParseKTVAddArgsPipes(t *testing.T) {
	song, err := parseKTVAddArgs("群青 | 日 | https://www.bilibili.com/video/BV1xx411c7mD")
	if err != nil {
		t.Fatalf("parseKTVAddArgs failed: %v", err)
	}
	if song.Name != "群青" || song.Category != "日" || song.BV != "https://www.bilibili.com/video/BV1xx411c7mD" {
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
