package banana

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/commandutil"
	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var (
	cooldownMu     sync.Mutex
	nextAvailable  = map[int64]time.Time{}
	defaultTimeout = 300 * time.Second

	dataURLPattern = regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)
	httpURLPattern = regexp.MustCompile(`https?://[^\s"\\]+`)
)

var errImageDownload = errors.New("banana image download failed")

type bananaContentPart struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *bananaContentImage `json:"image_url,omitempty"`
}

type bananaContentImage struct {
	URL       string `json:"url"`
	SourceURL string `json:"source_url,omitempty"`
}

type imageReference struct {
	DataURL   string
	SourceURL string
}

type bananaResult struct {
	imageBytes []byte
	imageURL   string
}

func init() {
	zero.OnMessage(commandutil.ReplyableCommandRule("banana-pro", "banana")).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		command, _ := ctx.State["command"].(string)
		handleBanana(ctx, command == "banana-pro")
	})

	zero.OnMessage(commandutil.ReplyableCommandRule("gi")).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		handleGI(ctx)
	})
}

func handleBanana(ctx *zero.Ctx, pro bool) {
	cfg := config.Get()
	if cfg == nil {
		ctx.SendChain(message.Text("配置尚未加载完成。"))
		return
	}

	prompt, _ := ctx.State["args"].(string)
	prompt = strings.TrimSpace(prompt)
	command := ".banana"
	if pro {
		command = ".banana-pro"
	}
	if prompt == "" {
		ctx.SendChain(message.Text("请提供 prompt，例如 " + command + " 梦幻森林。"))
		return
	}
	if cfg.Banana.APIKey == "" {
		ctx.SendChain(message.Text("尚未配置 BANANA_API_KEY / OpenAI API Key，无法调用图片生成。"))
		return
	}

	if wait := acquireCooldown(ctx.Event.UserID, cfg.Banana.CooldownSeconds); wait > 0 {
		ctx.SendChain(message.Text(fmt.Sprintf("香蕉工厂冷却中，请 %d 秒后再试。", wait)))
		return
	}

	httpClient := &http.Client{Timeout: timeoutFor(cfg.Banana.TimeoutSec)}
	contents, promptSummary, err := buildMessageContents(ctx, httpClient, prompt)
	if err != nil {
		if errors.Is(err, errImageDownload) {
			ctx.SendChain(message.Text("参考图片无法下载，请稍后再试。"))
			return
		}
		log.Warnf("[banana] build contents failed: %v", err)
		ctx.SendChain(message.Text(cfg.Banana.FailureReply))
		return
	}

	model := cfg.Banana.Model
	if pro && cfg.Banana.ProModel != "" {
		model = cfg.Banana.ProModel
	}

	result, err := callBananaAPI(httpClient, cfg, model, contents, promptSummary)
	if err != nil {
		log.Warnf("[banana] call api failed: %v", err)
		ctx.SendChain(message.Text(userFacingError(err)))
		return
	}

	reply := message.Text("完成啦！\nPrompt: " + promptSummary)
	if len(result.imageBytes) > 0 {
		ctx.SendChain(reply, message.ImageBytes(result.imageBytes))
		return
	}
	if result.imageURL != "" {
		ctx.SendChain(reply, message.Image(result.imageURL))
		return
	}
	ctx.SendChain(message.Text(cfg.Banana.FailureReply))
}

func buildMessageContents(ctx *zero.Ctx, httpClient *http.Client, prompt string) ([]bananaContentPart, string, error) {
	contents := []bananaContentPart{
		{Type: "text", Text: strings.TrimSpace(prompt)},
	}
	promptSummary := strings.TrimSpace(prompt)

	commandRefs, commandHadImage, err := extractImageReferences(httpClient, ctx.Event.Message)
	if err != nil && commandHadImage {
		return nil, "", err
	}
	if len(commandRefs) > 0 {
		return appendImageReferences(contents, commandRefs), promptSummary, nil
	}

	replyMsg := extractReplyMessage(ctx)
	replyRefs, replyHadImage, err := extractImageReferences(httpClient, replyMsg)
	if err != nil && replyHadImage {
		return nil, "", err
	}
	if len(replyRefs) > 0 {
		return appendImageReferences(contents, replyRefs), promptSummary, nil
	}
	if commandHadImage || replyHadImage {
		return nil, "", errImageDownload
	}
	return contents, promptSummary, nil
}

func extractReplyMessage(ctx *zero.Ctx) message.Message {
	var replyID interface{}
	for _, seg := range ctx.Event.Message {
		if seg.Type == "reply" {
			replyID = seg.Data["id"]
			break
		}
	}
	if replyID == nil {
		return nil
	}
	msg := ctx.GetMessage(replyID)
	return msg.Elements
}

func extractImageReferences(httpClient *http.Client, msg message.Message) ([]imageReference, bool, error) {
	if len(msg) == 0 {
		return nil, false, nil
	}

	refs := make([]imageReference, 0)
	hadImage := false
	var lastErr error
	for _, seg := range msg {
		if seg.Type != "image" {
			continue
		}
		hadImage = true
		ref, err := segmentToImageReference(httpClient, seg)
		if err != nil {
			lastErr = err
			continue
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 && lastErr != nil {
		return nil, hadImage, fmt.Errorf("%w: %v", errImageDownload, lastErr)
	}
	return refs, hadImage, nil
}

func segmentToImageReference(httpClient *http.Client, seg message.Segment) (imageReference, error) {
	if url := strings.TrimSpace(seg.Data["url"]); url != "" {
		data, contentType, err := downloadImage(httpClient, url)
		if err != nil {
			return imageReference{}, err
		}
		return imageReference{
			DataURL:   buildDataURL(data, contentType),
			SourceURL: url,
		}, nil
	}

	fileValue := strings.TrimSpace(seg.Data["file"])
	switch {
	case strings.HasPrefix(fileValue, "base64://"):
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(fileValue, "base64://"))
		if err != nil {
			return imageReference{}, err
		}
		return imageReference{DataURL: buildDataURL(data, "")}, nil
	case fileValue != "":
		if data, err := os.ReadFile(fileValue); err == nil {
			return imageReference{DataURL: buildDataURL(data, "")}, nil
		}
	}

	pathValue := strings.TrimSpace(seg.Data["path"])
	if pathValue != "" {
		if data, err := os.ReadFile(pathValue); err == nil {
			return imageReference{DataURL: buildDataURL(data, "")}, nil
		}
	}

	return imageReference{}, fmt.Errorf("unsupported image segment")
}

func appendImageReferences(contents []bananaContentPart, refs []imageReference) []bananaContentPart {
	for _, ref := range refs {
		item := bananaContentPart{
			Type: "image_url",
			ImageURL: &bananaContentImage{
				URL: ref.DataURL,
			},
		}
		if ref.SourceURL != "" {
			item.ImageURL.SourceURL = ref.SourceURL
		}
		contents = append(contents, item)
	}
	return contents
}

func callBananaAPI(httpClient *http.Client, cfg *config.Config, model string, contents []bananaContentPart, promptSummary string) (*bananaResult, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Banana.APIMode))
	switch mode {
	case "openai":
		return callOpenAIAPI(httpClient, cfg, model, contents)
	case "gemini":
		return callGeminiAPI(httpClient, cfg, model, contents)
	case "custom":
		return callCustomAPI(httpClient, cfg, model, promptSummary, contents)
	default:
		return nil, fmt.Errorf("unknown banana api mode: %s", cfg.Banana.APIMode)
	}
}

func callOpenAIAPI(httpClient *http.Client, cfg *config.Config, model string, contents []bananaContentPart) (*bananaResult, error) {
	endpoint := strings.TrimRight(cfg.Banana.APIBase, "/") + "/chat/completions"
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": toOpenAIContent(contents),
			},
		},
	}

	raw, err := postJSONWithRetries(httpClient, endpoint, cfg.Banana.APIKey, payload, cfg.Banana.MaxRetries)
	if err != nil {
		return nil, err
	}
	return decodeOpenAIResponse(raw, cfg.Banana.ResultFormat)
}

func callGeminiAPI(httpClient *http.Client, cfg *config.Config, model string, contents []bananaContentPart) (*bananaResult, error) {
	endpoint := strings.TrimRight(cfg.Banana.APIBase, "/") + "/v1beta/models/" + model + ":streamGenerateContent"
	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": toGeminiParts(contents),
			},
		},
	}

	raw, contentType, err := postGeminiWithRetries(httpClient, endpoint, cfg.Banana.APIKey, payload, cfg.Banana.MaxRetries)
	if err != nil {
		return nil, err
	}

	events, err := parseStreamingOrJSON(raw, contentType)
	if err != nil {
		return nil, err
	}
	return decodeGeminiEvents(httpClient, events, cfg.Banana.ResultFormat)
}

func callCustomAPI(httpClient *http.Client, cfg *config.Config, model, promptSummary string, contents []bananaContentPart) (*bananaResult, error) {
	endpoint := strings.TrimRight(cfg.Banana.APIBase, "/") + "/v1/draw/nano-banana"

	urls := make([]string, 0)
	for _, item := range contents {
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		if item.ImageURL.SourceURL != "" {
			urls = append(urls, item.ImageURL.SourceURL)
			continue
		}
		urls = append(urls, item.ImageURL.URL)
	}

	payload := map[string]any{
		"model":        model,
		"prompt":       promptSummary,
		"imageSize":    cfg.Banana.ImageSize,
		"shutProgress": true,
	}
	if len(urls) > 0 {
		payload["urls"] = urls
	}

	raw, contentType, err := postCustomWithRetries(httpClient, endpoint, cfg.Banana.APIKey, payload, cfg.Banana.MaxRetries)
	if err != nil {
		return nil, err
	}

	events, err := parseStreamingOrJSON(raw, contentType)
	if err != nil {
		return nil, err
	}
	return decodeCustomEvents(httpClient, events, cfg.Banana.ResultFormat)
}

func toOpenAIContent(contents []bananaContentPart) []map[string]any {
	result := make([]map[string]any, 0, len(contents))
	for _, item := range contents {
		switch item.Type {
		case "text":
			if item.Text == "" {
				continue
			}
			result = append(result, map[string]any{
				"type": "text",
				"text": item.Text,
			})
		case "image_url":
			if item.ImageURL == nil || item.ImageURL.URL == "" {
				continue
			}
			result = append(result, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": item.ImageURL.URL},
			})
		}
	}
	return result
}

func toGeminiParts(contents []bananaContentPart) []map[string]any {
	result := make([]map[string]any, 0, len(contents))
	for _, item := range contents {
		switch item.Type {
		case "text":
			if item.Text == "" {
				continue
			}
			result = append(result, map[string]any{"text": item.Text})
		case "image_url":
			if item.ImageURL == nil || item.ImageURL.URL == "" {
				continue
			}
			mimeType, data, ok := splitDataURL(item.ImageURL.URL)
			if !ok {
				continue
			}
			result = append(result, map[string]any{
				"inline_data": map[string]any{
					"mime_type": mimeType,
					"data":      data,
				},
			})
		}
	}
	return result
}

func postJSONWithRetries(httpClient *http.Client, endpoint, apiKey string, payload map[string]any, maxRetries int) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		return raw, nil
	}
	return nil, lastErr
}

func postGeminiWithRetries(httpClient *http.Client, endpoint, apiKey string, payload map[string]any, maxRetries int) ([]byte, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		url := endpoint
		if strings.Contains(endpoint, "generativelanguage.googleapis.com") {
			separator := "?"
			if strings.Contains(endpoint, "?") {
				separator = "&"
			}
			url = endpoint + separator + "key=" + apiKey
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		contentType := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		return raw, contentType, nil
	}
	return nil, "", lastErr
}

func postCustomWithRetries(httpClient *http.Client, endpoint, apiKey string, payload map[string]any, maxRetries int) ([]byte, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		contentType := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		return raw, contentType, nil
	}
	return nil, "", lastErr
}

func parseStreamingOrJSON(raw []byte, contentType string) ([]map[string]any, error) {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return parseSSEEvents(raw), nil
	}

	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		return []map[string]any{single}, nil
	}

	events := parseSSEEvents(raw)
	if len(events) > 0 {
		return events, nil
	}
	return nil, fmt.Errorf("unable to parse banana response")
}

func parseSSEEvents(raw []byte) []map[string]any {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	events := make([]map[string]any, 0)
	var payloadLines []string
	flush := func() {
		if len(payloadLines) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(payloadLines, "\n"))
		payloadLines = nil
		if text == "" || text == "[DONE]" {
			return
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(text), &event); err == nil {
			events = append(events, event)
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payloadLines = append(payloadLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return events
}

func decodeOpenAIResponse(raw []byte, resultFormat string) (*bananaResult, error) {
	var payload struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && len(payload.Data) > 0 {
		if payload.Data[0].B64JSON != "" {
			decoded, err := base64.StdEncoding.DecodeString(payload.Data[0].B64JSON)
			if err != nil {
				return nil, err
			}
			return &bananaResult{imageBytes: decoded}, nil
		}
		if payload.Data[0].URL != "" {
			return &bananaResult{imageURL: payload.Data[0].URL}, nil
		}
	}

	text := string(raw)
	if dataURL := dataURLPattern.FindString(text); dataURL != "" {
		_, encoded, ok := splitDataURL(dataURL)
		if !ok {
			return nil, fmt.Errorf("invalid data url")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		return &bananaResult{imageBytes: decoded}, nil
	}
	if resultFormat == "url" {
		if imageURL := httpURLPattern.FindString(text); imageURL != "" {
			return &bananaResult{imageURL: imageURL}, nil
		}
	}
	return nil, fmt.Errorf("openai response does not contain image data")
}

func decodeGeminiEvents(httpClient *http.Client, events []map[string]any, resultFormat string) (*bananaResult, error) {
	for i := len(events) - 1; i >= 0; i-- {
		candidates, ok := events[i]["candidates"].([]any)
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			candidateMap, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			content, ok := candidateMap["content"].(map[string]any)
			if !ok {
				continue
			}
			parts, ok := content["parts"].([]any)
			if !ok {
				continue
			}
			for _, part := range parts {
				partMap, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if inline, ok := partMap["inline_data"].(map[string]any); ok {
					if data, _ := inline["data"].(string); data != "" {
						decoded, err := base64.StdEncoding.DecodeString(data)
						if err != nil {
							return nil, err
						}
						return &bananaResult{imageBytes: decoded}, nil
					}
				}
				if inline, ok := partMap["inlineData"].(map[string]any); ok {
					if data, _ := inline["data"].(string); data != "" {
						decoded, err := base64.StdEncoding.DecodeString(data)
						if err != nil {
							return nil, err
						}
						return &bananaResult{imageBytes: decoded}, nil
					}
				}

				fileURL := ""
				if fileData, ok := partMap["file_data"].(map[string]any); ok {
					fileURL, _ = fileData["file_uri"].(string)
					if fileURL == "" {
						fileURL, _ = fileData["uri"].(string)
					}
				}
				if fileURL == "" {
					if fileData, ok := partMap["fileData"].(map[string]any); ok {
						fileURL, _ = fileData["fileUri"].(string)
						if fileURL == "" {
							fileURL, _ = fileData["uri"].(string)
						}
					}
				}
				if fileURL != "" {
					if resultFormat == "url" {
						return &bananaResult{imageURL: fileURL}, nil
					}
					data, _, err := downloadImage(httpClient, fileURL)
					if err != nil {
						return nil, err
					}
					return &bananaResult{imageBytes: data}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("gemini response does not contain image data")
}

func decodeCustomEvents(httpClient *http.Client, events []map[string]any, resultFormat string) (*bananaResult, error) {
	var final map[string]any
	for i := len(events) - 1; i >= 0; i-- {
		if _, ok := events[i]["results"]; ok {
			final = events[i]
			break
		}
	}
	if final == nil && len(events) > 0 {
		final = events[len(events)-1]
	}
	if final == nil {
		return nil, fmt.Errorf("custom response is empty")
	}

	if status, _ := final["status"].(string); status != "" && status != "success" && status != "succeeded" {
		return nil, fmt.Errorf("custom request failed: %s", status)
	}

	results, ok := final["results"].([]any)
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("custom response does not contain results")
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("custom result is invalid")
	}
	imageURL, _ := first["url"].(string)
	if imageURL == "" {
		return nil, fmt.Errorf("custom result does not contain url")
	}
	if resultFormat == "url" {
		return &bananaResult{imageURL: imageURL}, nil
	}
	data, _, err := downloadImage(httpClient, imageURL)
	if err != nil {
		return nil, err
	}
	return &bananaResult{imageBytes: data}, nil
}

func downloadImage(httpClient *http.Client, imageURL string) ([]byte, string, error) {
	resp, err := httpClient.Get(imageURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func buildDataURL(data []byte, contentType string) string {
	mimeType := contentType
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func splitDataURL(dataURL string) (string, string, bool) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", false
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := strings.TrimPrefix(parts[0], "data:")
	meta = strings.TrimSuffix(meta, ";base64")
	return meta, parts[1], true
}

func acquireCooldown(userID int64, cooldownSeconds int) int {
	if cooldownSeconds <= 0 {
		return 0
	}

	now := time.Now()
	cooldownMu.Lock()
	defer cooldownMu.Unlock()

	availableAt, ok := nextAvailable[userID]
	if ok && now.Before(availableAt) {
		return int(availableAt.Sub(now).Seconds()) + 1
	}
	nextAvailable[userID] = now.Add(time.Duration(cooldownSeconds) * time.Second)
	return 0
}

func timeoutFor(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

func imageFileName(imageURL string) string {
	fileName := filepath.Base(strings.Split(imageURL, "?")[0])
	if fileName == "." || fileName == "/" || fileName == "" {
		return "banana.png"
	}
	return fileName
}

func handleGI(ctx *zero.Ctx) {
	cfg := config.Get()
	if cfg == nil {
		ctx.SendChain(message.Text("配置尚未加载完成。"))
		return
	}

	prompt, _ := ctx.State["args"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		ctx.SendChain(message.Text("请提供 prompt，例如 .gi 梦幻森林。"))
		return
	}
	if cfg.Banana.APIKey == "" {
		ctx.SendChain(message.Text("尚未配置 BANANA_API_KEY，无法调用 gpt-image-2。"))
		return
	}

	if wait := acquireCooldown(ctx.Event.UserID, cfg.Banana.CooldownSeconds); wait > 0 {
		ctx.SendChain(message.Text(fmt.Sprintf("香蕉工厂冷却中，请 %d 秒后再试。", wait)))
		return
	}

	httpClient := &http.Client{Timeout: timeoutFor(cfg.Banana.TimeoutSec)}
	contents, promptSummary, err := buildMessageContents(ctx, httpClient, prompt)
	if err != nil {
		if errors.Is(err, errImageDownload) {
			ctx.SendChain(message.Text("参考图片无法下载，请稍后再试。"))
			return
		}
		log.Warnf("[banana] build contents for .gi failed: %v", err)
		ctx.SendChain(message.Text(cfg.Banana.FailureReply))
		return
	}

	result, err := callGIAPI(httpClient, cfg, contents, promptSummary)
	if err != nil {
		log.Warnf("[banana] .gi call failed: %v", err)
		ctx.SendChain(message.Text(userFacingError(err)))
		return
	}

	reply := message.Text("完成啦！\nPrompt: " + promptSummary)
	if len(result.imageBytes) > 0 {
		ctx.SendChain(reply, message.ImageBytes(result.imageBytes))
		return
	}
	if result.imageURL != "" {
		ctx.SendChain(reply, message.Image(result.imageURL))
		return
	}
	ctx.SendChain(message.Text(cfg.Banana.FailureReply))
}

func callGIAPI(httpClient *http.Client, cfg *config.Config, contents []bananaContentPart, promptSummary string) (*bananaResult, error) {
	model := cfg.Banana.GIModel
	if model == "" {
		model = "gpt-image-2"
	}
	size := cfg.Banana.GISize
	if size == "" {
		size = "auto"
	}
	pollInterval := cfg.Banana.GIPollIntervalSec
	if pollInterval <= 0 {
		pollInterval = 5
	}

	var urls []string
	for _, item := range contents {
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		if item.ImageURL.SourceURL != "" && isPublicHTTPURL(item.ImageURL.SourceURL) {
			urls = append(urls, item.ImageURL.SourceURL)
		}
	}

	endpoint := strings.TrimRight(cfg.Banana.APIBase, "/") + "/v1/draw/completions"
	payload := map[string]any{
		"model":        model,
		"prompt":       promptSummary,
		"size":         size,
		"webHook":      "-1",
		"shutProgress": true,
	}
	if len(urls) > 0 {
		payload["urls"] = urls
	}

	taskID, err := submitGITask(httpClient, endpoint, cfg.Banana.APIKey, payload, cfg.Banana.MaxRetries)
	if err != nil {
		return nil, err
	}

	resultEndpoint := strings.TrimRight(cfg.Banana.APIBase, "/") + "/v1/draw/result"
	timeout := timeoutFor(cfg.Banana.TimeoutSec)
	imageURL, err := pollGIResult(httpClient, resultEndpoint, cfg.Banana.APIKey, taskID, pollInterval, timeout)
	if err != nil {
		return nil, err
	}

	data, _, err := downloadImage(httpClient, imageURL)
	if err != nil {
		return nil, err
	}
	return &bananaResult{imageBytes: data}, nil
}

func submitGITask(httpClient *http.Client, endpoint, apiKey string, payload map[string]any, maxRetries int) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}

		var result struct {
			Code int `json:"code"`
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			lastErr = err
			continue
		}
		if result.Code != 0 {
			lastErr = fmt.Errorf("gi submit failed: code=%d msg=%s", result.Code, result.Msg)
			continue
		}
		if result.Data.ID == "" {
			lastErr = fmt.Errorf("gi submit returned empty task id")
			continue
		}
		return result.Data.ID, nil
	}
	return "", lastErr
}

func pollGIResult(httpClient *http.Client, endpoint, apiKey, taskID string, pollIntervalSec int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	payload := map[string]any{"id": taskID}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("gi poll timeout, task_id=%s", taskID)
		}

		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Duration(pollIntervalSec) * time.Second)
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			time.Sleep(time.Duration(pollIntervalSec) * time.Second)
			continue
		}

		var result struct {
			Code int `json:"code"`
			Data struct {
				Status        string `json:"status"`
				FailureReason string `json:"failure_reason"`
				Error         string `json:"error"`
				Results       []struct {
					URL string `json:"url"`
				} `json:"results"`
			} `json:"data"`
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			time.Sleep(time.Duration(pollIntervalSec) * time.Second)
			continue
		}
		if result.Code == -22 {
			return "", fmt.Errorf("gi task not found, id=%s", taskID)
		}
		if result.Code != 0 {
			return "", fmt.Errorf("gi poll failed: code=%d msg=%s", result.Code, result.Msg)
		}

		status := strings.ToLower(strings.TrimSpace(result.Data.Status))
		switch status {
		case "succeeded":
			if len(result.Data.Results) == 0 || result.Data.Results[0].URL == "" {
				return "", fmt.Errorf("gi result missing image url")
			}
			return result.Data.Results[0].URL, nil
		case "failed":
			reason := result.Data.FailureReason
			if reason == "" {
				reason = result.Data.Error
			}
			return "", fmt.Errorf("gi generation failed: %s", reason)
		}

		time.Sleep(time.Duration(pollIntervalSec) * time.Second)
	}
}

func isPublicHTTPURL(url string) bool {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return false
	}
	return !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "localhost")
}

func userFacingError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "图片生成超时了，请稍后再试。"
	case strings.Contains(lower, "content_policy") || strings.Contains(lower, "content policy") ||
		strings.Contains(lower, "safety") || strings.Contains(lower, "blocked"):
		return "内容不合规，请修改 prompt 后重试。"
	case strings.Contains(lower, "status 429") || strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit"):
		return "请求过于频繁，请稍后再试。"
	case strings.Contains(lower, "status 401") || strings.Contains(lower, "status 403") ||
		strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden"):
		return "API 认证失败，请联系管理员。"
	case strings.Contains(lower, "status 402") || strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "billing"):
		return "API 额度不足，请联系管理员。"
	case strings.Contains(lower, "status 5"):
		return "上游服务异常，请稍后再试。"
	default:
		return "图片生成失败：" + msg
	}
}
