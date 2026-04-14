package stickersaver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestFindReplyMessageID(t *testing.T) {
	msgID, ok := findReplyMessageID(message.Message{
		message.Reply("42"),
		message.Text("save"),
	})
	if !ok || msgID != "42" {
		t.Fatalf("unexpected reply id: %v ok=%v", msgID, ok)
	}
}

func TestFindFirstImageURL(t *testing.T) {
	url := findFirstImageURL(message.Message{
		message.Text("x"),
		{Type: "image", Data: map[string]string{"url": "https://example.com/a.png"}},
	})
	if url != "https://example.com/a.png" {
		t.Fatalf("unexpected image url: %q", url)
	}
}

func TestSaveStickerToDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png"))
	}))
	defer server.Close()

	dir := t.TempDir()
	hash, savePath, err := saveStickerToDir(dir, server.Client(), server.URL+"/sticker.png")
	if err != nil {
		t.Fatalf("saveStickerToDir returned error: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected non-empty hash")
	}
	if filepath.Ext(savePath) != ".png" {
		t.Fatalf("unexpected save path: %s", savePath)
	}
	if _, err := os.Stat(savePath); err != nil {
		t.Fatalf("expected sticker to be written: %v", err)
	}
}

func TestDetectStickerExtFallback(t *testing.T) {
	if got := detectStickerExt("", "https://example.com/a.jpeg?x=1"); got != ".jpg" {
		t.Fatalf("unexpected ext for jpeg url: %s", got)
	}
	if got := detectStickerExt("", "https://example.com/a.unknown"); got != ".png" {
		t.Fatalf("unexpected fallback ext: %s", got)
	}
}
