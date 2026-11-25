package omoi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/config"
)

func TestFetchFileCachesAndBounds(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/files/small":
			_, _ = io.WriteString(w, "GIF89a")
		case "/files/big":
			_, _ = w.Write(make([]byte, fileLimit+1))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := &OmoiClient{baseURL: server.URL, apiKey: "k", httpClient: server.Client()}
	for i := 0; i < 2; i++ {
		data, err := c.fetchFile(context.Background(), "small")
		if err != nil || string(data) != "GIF89a" {
			t.Fatalf("fetch: %q %v", data, err)
		}
	}
	if calls != 1 {
		t.Fatalf("not cached: %d calls", calls)
	}
	if _, err := c.fetchFile(context.Background(), "big"); err == nil {
		t.Fatal("oversized file accepted")
	}
	if _, err := c.fetchFile(context.Background(), "missing"); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, ok := c.imageMessage(context.Background(), deliveryPart{FileID: "missing"}); ok {
		t.Fatal("failed image not skipped")
	}
	sticker, _ := c.imageMessage(context.Background(), deliveryPart{FileID: "small", Sticker: true})
	picture, _ := c.imageMessage(context.Background(), deliveryPart{FileID: "small"})
	if sticker.Data["sub_type"] != "1" || sticker.Data["summary"] != "[动画表情]" || picture.Data["sub_type"] != "" {
		t.Fatalf("sticker %v, picture %v", sticker.Data, picture.Data)
	}
}

func TestImagePartKinds(t *testing.T) {
	r := replyFromBlocks([]ContentBlock{
		{Type: "image", FileID: "s", Text: "猫猫点头"},
		{Type: "image", FileID: "p", Text: "配图：新专辑封面"},
	})
	if len(r) != 2 || !r[0].Sticker || r[1].Sticker {
		t.Fatalf("parts %+v", r)
	}
	parts := replyParts(r, config.OmoiConfig{})
	if !parts[0].Sticker || parts[1].Sticker {
		t.Fatalf("delivery parts %+v", parts)
	}
}

func TestCollectStickerOutcomes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.URL.Path != "/agents/a/stickers/collect" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.FormValue("actor_id") {
		case "1":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"name":"`+r.FormValue("name")+`"}`)
		case "2":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"not a sticker collector for this agent"}`)
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"error":"media_too_large"}`)
		}
	}))
	defer server.Close()
	c := &OmoiClient{baseURL: server.URL, apiKey: "k", agentID: "a", httpClient: server.Client()}
	if name, err := c.collectSticker(context.Background(), strings.NewReader("img"), "狗头", "得意", "1"); err != nil || name != "狗头" {
		t.Fatalf("collect: %q %v", name, err)
	}
	if _, err := c.collectSticker(context.Background(), strings.NewReader("img"), "狗头", "", "2"); err != errNotCollector {
		t.Fatalf("non-collector: %v", err)
	}
	if _, err := c.collectSticker(context.Background(), strings.NewReader("img"), "狗头", "", "3"); err == nil || err.Error() != "media_too_large" {
		t.Fatalf("rejected upload: %v", err)
	}
}
