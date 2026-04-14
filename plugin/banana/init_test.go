package banana

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestParseSSEEvents(t *testing.T) {
	raw := []byte("data: {\"status\":\"running\"}\n\ndata: {\"results\":[{\"url\":\"https://example.com/a.png\"}]}\n\n")
	events := parseSSEEvents(raw)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestDecodeOpenAIResponseDataURL(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"data:image/png;base64,aGVsbG8="}}]}`)
	result, err := decodeOpenAIResponse(raw, "b64_json")
	if err != nil {
		t.Fatalf("decodeOpenAIResponse returned error: %v", err)
	}
	if string(result.imageBytes) != "hello" {
		t.Fatalf("unexpected decoded payload: %q", string(result.imageBytes))
	}
}

func TestSegmentToImageReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()

	ref, err := segmentToImageReference(server.Client(), message.Segment{
		Type: "image",
		Data: map[string]string{"url": server.URL + "/a.png"},
	})
	if err != nil {
		t.Fatalf("segmentToImageReference returned error: %v", err)
	}
	if ref.SourceURL == "" || ref.DataURL == "" {
		t.Fatalf("unexpected image reference: %#v", ref)
	}
}

func TestSplitDataURL(t *testing.T) {
	mimeType, payload, ok := splitDataURL("data:image/png;base64,aGVsbG8=")
	if !ok {
		t.Fatalf("expected valid data url")
	}
	if mimeType != "image/png" || payload != "aGVsbG8=" {
		t.Fatalf("unexpected split result: %q %q", mimeType, payload)
	}
}
