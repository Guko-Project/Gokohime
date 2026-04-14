package stickersaver

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/commandutil"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const saveDir = "data/stickers"

var client = &http.Client{Timeout: 15 * time.Second}

func init() {
	_ = os.MkdirAll(saveDir, 0o755)

	zero.OnMessage(commandutil.ReplyableCommandRule("save", "保存图片", "保存表情", "保存")).
		SetBlock(true).
		Handle(handleSaveSticker)
}

func handleSaveSticker(ctx *zero.Ctx) {
	replyMsgID, ok := findReplyMessageID(ctx.Event.Message)
	if !ok {
		ctx.SendChain(message.Text("只有回复表情才可以用捏"))
		return
	}

	repliedMsg := ctx.GetMessage(replyMsgID)
	if repliedMsg.MessageID.ID() == 0 {
		ctx.SendChain(message.Text("获取消息失败"))
		return
	}

	imageURL := findFirstImageURL(repliedMsg.Elements)
	if imageURL == "" {
		ctx.SendChain(message.Text("未在回复内容中检测到表情..."))
		return
	}

	if _, _, err := saveStickerToDir(saveDir, client, imageURL); err != nil {
		log.Warnf("[stickersaver] save sticker failed: %v", err)
		ctx.SendChain(message.Text("保存失败"))
		return
	}

	ctx.SendChain(
		message.Text("表情："),
		message.Image(imageURL),
		message.Text("\n原始链接："+imageURL),
	)
}

func findReplyMessageID(segments message.Message) (interface{}, bool) {
	for _, seg := range segments {
		if seg.Type == "reply" && seg.Data["id"] != "" {
			return seg.Data["id"], true
		}
	}
	return nil, false
}

func findFirstImageURL(segments message.Message) string {
	for _, seg := range segments {
		if seg.Type == "image" && seg.Data["url"] != "" {
			return seg.Data["url"]
		}
	}
	return ""
}

func saveStickerToDir(dir string, httpClient *http.Client, imageURL string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	resp, err := httpClient.Get(imageURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(data))
	ext := detectStickerExt(resp.Header.Get("Content-Type"), imageURL)
	savePath := filepath.Join(dir, hash+ext)

	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		return "", "", err
	}
	return hash, savePath, nil
}

func detectStickerExt(contentType, rawURL string) string {
	switch {
	case strings.Contains(contentType, "image/jpeg"):
		return ".jpg"
	case strings.Contains(contentType, "image/gif"):
		return ".gif"
	case strings.Contains(contentType, "image/webp"):
		return ".webp"
	case strings.Contains(contentType, "image/png"):
		return ".png"
	}

	ext := strings.ToLower(filepath.Ext(strings.Split(rawURL, "?")[0]))
	switch ext {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".gif", ".png", ".webp":
		return ext
	default:
		return ".png"
	}
}
