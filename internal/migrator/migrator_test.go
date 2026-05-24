package migrator

import (
	"encoding/json"
	"testing"
)

func TestKTVSongSourceLegacyCategoryField(t *testing.T) {
	raw := []byte(`{"歌名":{"bv":"https://example.com","catagory":"中","issuer":"1"}}`)

	var data map[string]ktvSongSource
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	item := data["歌名"]
	if item.Category == nil || *item.Category != "中" {
		t.Fatalf("expected legacy catagory field to map into Category, got %#v", item.Category)
	}
}
