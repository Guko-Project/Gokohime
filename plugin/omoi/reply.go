package omoi

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// replyPart is one piece of an Omoi answer in send order: text, or an image
// stored in Omoi (a sticker or a shared picture).
type replyPart struct {
	Text   string
	FileID string
	Mime   string
	// Sticker sends the image the way QQ users send stickers.
	Sticker bool
}

// sharedImagePrefix marks the text of a picture Omoi shares (not a sticker).
const sharedImagePrefix = "配图："

// imagePart reads an image Omoi sends; anything but a shared picture is a sticker.
func imagePart(b ContentBlock) replyPart {
	return replyPart{FileID: b.FileID, Mime: b.MediaType, Sticker: !strings.HasPrefix(b.Text, sharedImagePrefix)}
}

// Reply is an answer as Omoi streamed it, text and images in order.
type Reply []replyPart

// Text is the answer without its images.
func (r Reply) Text() string {
	var b strings.Builder
	for _, p := range r {
		b.WriteString(p.Text)
	}
	return b.String()
}

func textReply(text string) Reply { return Reply{{Text: text}} }

// stickersAtEdges keeps stickers out of the middle of an answer: those before
// any text open it, the rest close it. Shared pictures stay where they are.
func (r Reply) stickersAtEdges() Reply {
	var out, last Reply
	texted := false
	for _, p := range r {
		switch {
		case p.Sticker && texted:
			last = append(last, p)
		default:
			texted = texted || strings.TrimSpace(p.Text) != ""
			out = append(out, p)
		}
	}
	return append(out, last...)
}

// replyFromBlocks reads a scheduled delivery's ordered content.
func replyFromBlocks(blocks []ContentBlock) Reply {
	var r Reply
	for _, b := range blocks {
		switch {
		case b.Type == "text" && b.Text != "":
			r = append(r, replyPart{Text: b.Text})
		case b.Type == "image" && b.FileID != "":
			r = append(r, imagePart(b))
		}
	}
	return r
}

// imageDelay separates an image from the message before it.
const imageDelay = time.Second

// replyParts turns an answer into QQ messages with typing delays. Text is split
// as before; each image is its own message. [SKIP] withholds everything.
func replyParts(r Reply, cfg config.OmoiConfig) []deliveryPart {
	if cfg.SkipMarker != "" && strings.Contains(r.Text(), cfg.SkipMarker) {
		return nil
	}
	var parts []deliveryPart
	var delay time.Duration
	for _, p := range r.stickersAtEdges() {
		if p.FileID != "" {
			if len(parts) > 0 {
				delay += imageDelay
			}
			parts = append(parts, deliveryPart{FileID: p.FileID, Mime: p.Mime, Sticker: p.Sticker, Delay: delay})
			delay = 0
			continue
		}
		before := len(parts)
		sendSplitReply(p.Text, cfg, func(text string) {
			// Text after an image waits as if typed; sendSplitReply only delays
			// between its own segments.
			if len(parts) == before && before > 0 {
				delay += calcTypingDelay(text, cfg)
			}
			parts = append(parts, deliveryPart{Text: text, Delay: delay})
			delay = 0
		}, func(d time.Duration) { delay += d })
	}
	return parts
}

// fileCache keeps recently sent images so common stickers are fetched once.
var fileCache = struct {
	sync.Mutex
	data  map[string][]byte
	order []string
}{data: map[string][]byte{}}

const (
	fileCacheSize = 64
	fileLimit     = 2 << 20
)

// fetchFile downloads an Omoi upload the API key's tenant can read.
func (c *OmoiClient) fetchFile(ctx context.Context, id string) ([]byte, error) {
	fileCache.Lock()
	if data, ok := fileCache.data[id]; ok {
		fileCache.Unlock()
		return data, nil
	}
	fileCache.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/files/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, fileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > fileLimit {
		return nil, fmt.Errorf("file larger than %d MiB", fileLimit>>20)
	}
	fileCache.Lock()
	if _, ok := fileCache.data[id]; !ok {
		fileCache.data[id] = data
		fileCache.order = append(fileCache.order, id)
		if len(fileCache.order) > fileCacheSize {
			delete(fileCache.data, fileCache.order[0])
			fileCache.order = fileCache.order[1:]
		}
	}
	fileCache.Unlock()
	return data, nil
}

// imageMessage fetches an image part; a failure is logged and the part skipped.
func (c *OmoiClient) imageMessage(ctx context.Context, part deliveryPart) (message.Segment, bool) {
	data, err := c.fetchFile(ctx, part.FileID)
	if err != nil {
		log.Warnf("[omoi] trace=%s stage=sticker_failed file=%s error=%v", traceID(ctx), part.FileID, err)
		return message.Segment{}, false
	}
	image := message.Image("base64://" + base64.StdEncoding.EncodeToString(data))
	if part.Sticker {
		// QQ shows sub_type 1 as a sticker, like the ones people send.
		image.Data["sub_type"] = "1"
		image.Data["summary"] = "[动画表情]"
	}
	return image, true
}
