package omoi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type ContentBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	FileID string `json:"file_id,omitempty"`
}
type mediaCache struct {
	mu     sync.Mutex
	blocks []ContentBlock
	at     time.Time
}
type MediaReference struct {
	Kind, URL, File, Label string
	Historical             bool
	cache                  *mediaCache
}
type traceKey struct{}

func requestContext() context.Context {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return context.WithValue(context.Background(), traceKey{}, hex.EncodeToString(b))
}
func traceID(ctx context.Context) string { s, _ := ctx.Value(traceKey{}).(string); return s }

func reportFailure(ctx *zero.Ctx, requestCtx context.Context, err error) {
	log.Warnf("[omoi] trace=%s stage=reply_failed error=%v", traceID(requestCtx), err)
	text := "本次对话处理失败，请稍后重试。"
	if strings.Contains(err.Error(), "media_") || strings.Contains(err.Error(), "video_") || strings.Contains(err.Error(), "image_") {
		text = "附件处理失败（可能过大、已过期或格式不支持），请裁剪或重新发送。"
	}
	ctx.Send(text + " 排查编号：" + traceID(requestCtx))
}

func mediaFromSegments(segments message.Message, label string) []MediaReference {
	var refs []MediaReference
	for _, seg := range segments {
		if seg.Type == "image" || seg.Type == "video" {
			refs = append(refs, MediaReference{Kind: seg.Type, URL: seg.Data["url"], File: seg.Data["file"], Label: label, cache: &mediaCache{}})
		}
	}
	return refs
}

func extractMedia(ctx *zero.Ctx, withReply bool) []MediaReference {
	refs := mediaFromSegments(ctx.Event.Message, "当前消息附件")
	if withReply {
		for _, seg := range ctx.Event.Message {
			if seg.Type == "reply" && seg.Data["id"] != "" {
				original := ctx.GetMessage(seg.Data["id"], true)
				refs = append(refs, mediaFromSegments(original.Elements, "引用消息附件")...)
				break
			}
		}
	}
	// Resolve missing OneBot URLs without treating a NapCat path as a local file.
	if withReply {
		for i := range refs {
			r := &refs[i]
			if r.URL == "" && r.File != "" && !strings.HasPrefix(r.File, "base64://") && !strings.HasPrefix(r.File, "http") {
				var action string
				if r.Kind == "image" {
					action = "get_image"
				} else {
					action = "get_video"
				}
				resolveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				data := ctx.CallActionWithContext(resolveCtx, action, zero.Params{"file": r.File}).Data
				cancel()
				r.URL = data.Get("url").String()
			}
		}
	}
	return refs
}

func describeMedia(text string, refs []MediaReference) string {
	if len(refs) == 0 {
		return text
	}
	return text + fmt.Sprintf("\n[本条消息包含 %d 个图片/视频附件]", len(refs))
}

// Current/replied attachments are mandatory. Historical ones are bounded and labelled.
func selectMedia(current []MediaReference, history []BufferedMessage) []MediaReference {
	out := append([]MediaReference(nil), current...)
	seen := map[string]bool{}
	videos := 0
	key := func(r MediaReference) string {
		if r.File != "" {
			return r.Kind + ":" + r.File
		}
		return r.Kind + ":" + r.URL
	}
	for _, ref := range current {
		seen[key(ref)] = true
		if ref.Kind == "video" {
			videos++
		}
	}
	for i := len(history) - 1; i >= 0 && len(out) < 4; i-- {
		for _, ref := range history[i].Media {
			if len(out) >= 4 {
				break
			}
			if seen[key(ref)] || ref.Kind == "video" && videos >= 1 {
				continue
			}
			seen[key(ref)] = true
			if ref.Kind == "video" {
				videos++
			}
			ref.Historical = true
			ref.Label = fmt.Sprintf("历史消息 %s(%d) %s 的附件", history[i].Nickname, history[i].UserID, history[i].Time.Format(timeFormat))
			out = append(out, ref)
		}
	}
	return out
}

func safeMediaURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("media_url_invalid")
	}
	h := strings.ToLower(u.Hostname())
	// Current QQ NT attachments use qq.com.cn; keep this exception host-specific.
	allowed := h == "multimedia.nt.qq.com.cn"
	for _, domain := range []string{"qq.com", "qpic.cn", "gtimg.cn", "gtimg.com"} {
		if h == domain || strings.HasSuffix(h, "."+domain) {
			allowed = true
		}
	}
	if !allowed {
		return nil, fmt.Errorf("media_url_host_not_allowed")
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return nil, fmt.Errorf("media_url_port_not_allowed")
	}
	return u, nil
}

func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15"} {
		_, n, _ := net.ParseCIDR(cidr)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

var mediaHTTP = &http.Client{Timeout: 40 * time.Second,
	Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 15 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("media_dns_failed")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fmt.Errorf("media_address_not_public")
			}
		}
		for _, ip := range ips {
			c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return c, nil
			}
		}
		return nil, fmt.Errorf("media_connect_failed")
	}},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("media_redirect_limit")
		}
		_, err := safeMediaURL(req.URL.String())
		return err
	},
}

func downloadMedia(ctx context.Context, ref MediaReference) (*os.File, error) {
	limit := int64(20 << 20)
	if ref.Kind == "video" {
		limit = 50 << 20
	}
	var reader io.Reader
	var body io.Closer
	raw := ref.URL
	if raw == "" && strings.HasPrefix(ref.File, "http") {
		raw = ref.File
	}
	if raw != "" {
		u, err := safeMediaURL(raw)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("media_url_invalid")
		}
		resp, err := mediaHTTP.Do(req)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return nil, fmt.Errorf("media_download_timeout")
			}
			return nil, fmt.Errorf("media_download_failed: %T", err)
		}
		body = resp.Body
		defer body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("media_source_http_error: HTTP %d", resp.StatusCode)
		}
		if resp.ContentLength > limit {
			return nil, fmt.Errorf("media_too_large: maximum %d MiB", limit>>20)
		}
		reader = resp.Body
		log.Infof("[omoi] trace=%s stage=download host=%s kind=%s", traceID(ctx), u.Hostname(), ref.Kind)
	} else if strings.HasPrefix(ref.File, "base64://") {
		if int64(len(ref.File)) > (limit+2)/3*4+16 {
			return nil, fmt.Errorf("media_too_large")
		}
		reader = base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimPrefix(ref.File, "base64://")))
	} else {
		return nil, fmt.Errorf("media_source_unavailable: QQ did not provide a downloadable URL")
	}
	f, err := os.CreateTemp("", "omoi-media-")
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, io.LimitReader(reader, limit+1))
	if err != nil || n > limit {
		f.Close()
		os.Remove(f.Name())
		return nil, fmt.Errorf("media_download_failed_or_too_large")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	log.Infof("[omoi] trace=%s stage=download_complete bytes=%d", traceID(ctx), n)
	return f, nil
}

func (c *OmoiClient) uploadMedia(ctx context.Context, ref MediaReference) ([]ContentBlock, error) {
	if ref.cache != nil {
		ref.cache.mu.Lock()
		defer ref.cache.mu.Unlock()
		if len(ref.cache.blocks) > 0 && time.Since(ref.cache.at) < 23*time.Hour {
			return ref.cache.blocks, nil
		}
	}
	f, err := downloadMedia(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	pr, pw := io.Pipe()
	defer pr.Close()
	w := multipart.NewWriter(pw)
	go func() {
		var err error
		defer func() { _ = pw.CloseWithError(err) }()
		if err = w.WriteField("kind", ref.Kind); err != nil {
			return
		}
		var part io.Writer
		part, err = w.CreateFormFile("file", "attachment")
		if err != nil {
			return
		}
		_, err = io.Copy(part, f)
		if err == nil {
			err = w.Close()
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/files", pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Request-ID", traceID(ctx))
	resp, err := (&http.Client{Timeout: 170 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("media_upload_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("media_upload_failed: HTTP %d %s", resp.StatusCode, body)
	}
	var result struct {
		Content []ContentBlock `json:"content"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil || len(result.Content) == 0 {
		return nil, fmt.Errorf("media_upload_invalid_response")
	}
	if ref.cache != nil {
		ref.cache.blocks = result.Content
		ref.cache.at = time.Now()
	}
	return result.Content, nil
}

func (c *OmoiClient) prepareAttachments(ctx context.Context, refs []MediaReference) ([]ContentBlock, error) {
	if len(refs) > 4 {
		return nil, fmt.Errorf("media_count_limit: send at most four attachments")
	}
	var out []ContentBlock
	videos := 0
	for _, ref := range refs {
		if ref.Kind == "video" {
			videos++
			if videos > 1 {
				return nil, fmt.Errorf("media_video_count_limit: send one video at a time")
			}
		}
		blocks, err := c.uploadMedia(ctx, ref)
		if err != nil {
			log.Warnf("[omoi] trace=%s stage=media_failed error=%v", traceID(ctx), err)
			if ref.Historical {
				out = append(out, ContentBlock{Type: "text", Text: "\n[" + ref.Label + "未能载入，请勿猜测其内容]"})
				continue
			}
			return nil, err
		}
		out = append(out, ContentBlock{Type: "text", Text: "\n" + ref.Label + ":\n"})
		out = append(out, blocks...)
	}
	return out, nil
}
