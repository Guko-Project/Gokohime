package omoi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

var errNotCollector = fmt.Errorf("not a sticker collector")

// handleSticker adds a quoted group image to the agent's sticker library:
// `.sticker add 名字 用途`. Omoi decides who may; others are ignored silently.
func handleSticker(ctx *zero.Ctx) {
	ensureClient()
	if client == nil || ctx.Event.GroupID == 0 {
		return
	}
	args := strings.Fields(strings.TrimSpace(fmt.Sprint(ctx.State["args"])))
	if len(args) < 2 || args[0] != "add" {
		ctx.Send("用法：引用一张图片，发送 .sticker add 名字 用途")
		return
	}
	name, usage := args[1], strings.Join(args[2:], " ")
	var image *MediaReference
	for _, ref := range extractMedia(ctx, true) {
		if ref.Kind == "image" && ref.Label == "引用消息附件" {
			image = &ref
			break
		}
	}
	if image == nil {
		ctx.Send("请引用一张图片再发送 .sticker add 名字 用途")
		return
	}
	reqCtx, cancel := context.WithTimeout(requestContext(), 60*time.Second)
	defer cancel()
	f, err := downloadMedia(reqCtx, *image)
	if err != nil {
		log.Warnf("[omoi] trace=%s stage=sticker_collect error=%v", traceID(reqCtx), err)
		ctx.Send("图片下载失败，请重新发送图片后再试。")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	saved, err := client.collectSticker(reqCtx, f, name, usage, strconv.FormatInt(ctx.Event.UserID, 10))
	switch {
	case err == errNotCollector:
		log.Infof("[omoi] sticker collect ignored: user=%d", ctx.Event.UserID)
	case err != nil:
		log.Warnf("[omoi] trace=%s stage=sticker_collect error=%v", traceID(reqCtx), err)
		ctx.Send("收藏失败：" + err.Error())
	default:
		ctx.Send("已收藏：" + saved)
	}
}

func (c *OmoiClient) collectSticker(ctx context.Context, file io.Reader, name, usage, actor string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range map[string]string{"name": name, "description": usage, "actor_id": actor} {
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}
	part, err := w.CreateFormFile("file", "sticker")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, io.LimitReader(file, fileLimit+1)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/agents/"+c.agentID+"/stickers/collect", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Request-ID", traceID(ctx))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
	switch resp.StatusCode {
	case http.StatusCreated:
		return result.Name, nil
	case http.StatusForbidden:
		return "", errNotCollector
	}
	if result.Error == "" {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return "", fmt.Errorf("%s", result.Error)
}
