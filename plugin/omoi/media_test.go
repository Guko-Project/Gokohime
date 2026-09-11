package omoi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestMediaExtractionAndURLBoundary(t *testing.T) {
	refs := mediaFromSegments(message.Message{{Type: "text", Data: map[string]string{"text": "hi"}}, {Type: "image", Data: map[string]string{"url": "https://gchat.qpic.cn/a"}}, {Type: "video", Data: map[string]string{"url": "https://multimedia.nt.qq.com/a"}}}, "test")
	if len(refs) != 2 || refs[0].Kind != "image" || refs[1].Kind != "video" {
		t.Fatalf("lost media: %+v", refs)
	}
	for _, url := range []string{"file:///etc/passwd", "http://127.0.0.1/a", "https://qpic.cn.evil.example/a", "https://user:pass@gchat.qpic.cn/a", "https://gchat.qpic.cn:8080/a", "https://multimedia.nt.qq.com.cn.evil.example/a", "https://other.qq.com.cn/a", "https://evil.multimedia.nt.qq.com.cn/a"} {
		if _, err := safeMediaURL(url); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatalf("unsafe IP %s", ip)
		}
	}
	if _, err := safeMediaURL(refs[0].URL); err != nil {
		t.Fatal(err)
	}
	if _, err := safeMediaURL("https://multimedia.nt.qq.com.cn/download?appid=1407&fileid=fixture&rkey=test"); err != nil {
		t.Fatalf("rejected QQ NT media host: %v", err)
	}
}

func TestUploadReferencesAreReusedAcrossChatRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Request-ID") == "" {
			t.Error("missing trace")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("kind") != "image" {
			t.Error("missing kind")
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != "fixture bytes" {
			t.Error("wrong bytes")
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{"content": []ContentBlock{{Type: "image", FileID: "file123"}}})
	}))
	defer server.Close()
	c := &OmoiClient{baseURL: server.URL, apiKey: "test"}
	ref := MediaReference{Kind: "image", File: "base64://" + base64.StdEncoding.EncodeToString([]byte("fixture bytes")), cache: &mediaCache{}}
	ctx := requestContext()
	for i := 0; i < 2; i++ {
		blocks, err := c.prepareAttachments(ctx, []MediaReference{ref})
		if err != nil || len(blocks) != 2 || blocks[1].FileID != "file123" {
			t.Fatalf("attachment serialization: %+v %v", blocks, err)
		}
	}
	if calls != 1 {
		t.Fatalf("uploaded %d times", calls)
	}
}

func TestMediaFailuresAndSSETruncation(t *testing.T) {
	if _, err := downloadMedia(context.Background(), MediaReference{Kind: "image", File: "/etc/passwd"}); err == nil {
		t.Fatal("read arbitrary local file")
	}
	f, err := downloadMedia(context.Background(), MediaReference{Kind: "image", File: "base64://YQ=="})
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())
	if _, err := (&OmoiClient{}).consumeSSE(strings.NewReader("event: delta\ndata: {\"content\":\"partial\"}\n\n")); err == nil {
		t.Fatal("truncated SSE accepted")
	}
}
