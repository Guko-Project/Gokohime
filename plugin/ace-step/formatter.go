package acestep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/colanns/gokohime/internal/log"
)

const deepSeekRequestTimeout = 120 * time.Second

const deepSeekSystemPrompt = `你是 ACE-Step 1.5 音乐生成规划器

你的任务是把用户的音乐需求改写成适合 ACE-Step legacy /release_task 直接渲染的结构化 JSON

你熟悉 ACE-Step 1.5 的控制方式：
caption 描述整体音乐画像，包括风格、流派、情绪、氛围、乐器、音色质感、制作风格、人声特点、时代参考、结构倾向
lyrics 描述时间脚本，包括歌词、段落结构、演唱方式、器乐段落、能量变化、开始和结束方式
bpm、key_scale、time_signature、vocal_language、instrumental、duration 是独立元数据，不要写进 caption

你也熟悉 The Complete Guide to Mastering Suno 中的专业音乐提示词思路，请运用其中关于结构标签、风格锚点、动态演进、可唱歌词、段落控制、重复强化、避免冲突提示的原则，但不要提及文章名

输出要求：
只输出 JSON 对象
不要输出 markdown
不要输出解释
不要输出思考过程
不要把 duration、具体秒数、BPM、调性、拍号写进 caption
不要虚构真实歌手名的仿作请求，若用户要求某歌手风格，改写为抽象音乐特征
如果 has_explicit_duration 为 true，可以省略 duration 或输出 null
如果 has_explicit_duration 为 false，可以根据歌曲结构推断 duration，但不要超过 180

JSON schema：
{
  "caption": "string",
  "lyrics": "string",
  "bpm": 120,
  "key_scale": "C major",
  "time_signature": "4/4",
  "vocal_language": "zh|ja|en|unknown",
  "instrumental": false,
  "duration": 120
}

caption 写作要求：
具体优于模糊
组合多个维度
覆盖风格、情绪、乐器、音色、人声、制作、结构等可控维度
避免互相冲突的风格堆叠
如果用户需求很短，补全为可生成的音乐画像
如果用户需要惊喜，可以保留适度自由度
参考下面这种 caption 密度和专业粒度，但不要固定复刻内容：
A mid-tempo Mandopop ballad built on a steady electronic drum machine groove and a clean synth bassline. The arrangement features layered synthesizers, including chordal pads and melodic counterpoints, complemented by a clean electric guitar playing arpeggiated figures. The emotional male lead vocal, sung in Mandarin, soars through the verses and choruses, often reaching into a powerful falsetto. The track includes an expressive, melodic electric guitar solo and a dynamic bridge that builds tension before a final, passionate chorus filled with vocal ad-libs and a climactic guitar flourish.

lyrics 写作要求：
如果用户提供歌词，保留核心文本和语义，只做结构化、断行、补充必要段落标签
如果用户没提供歌词但不是纯音乐，创作完整歌词
如果是纯音乐，输出 [Instrumental] 或器乐结构标签
每段之间空行分隔
标签简洁，不要堆叠过多修饰
避免 AI 味歌词：空泛意象堆砌、混乱押韵、段落边界模糊、隐喻混用、每行太长不可唱`

type deepSeekFormatResult struct {
	Caption       string  `json:"caption"`
	Lyrics        string  `json:"lyrics"`
	BPM           int     `json:"bpm"`
	KeyScale      string  `json:"key_scale"`
	TimeSignature string  `json:"time_signature"`
	VocalLanguage string  `json:"vocal_language"`
	Instrumental  bool    `json:"instrumental"`
	Duration      float64 `json:"duration"`
}

type deepSeekUserInput struct {
	UserPrompt          string `json:"user_prompt"`
	UserLyrics          string `json:"user_lyrics,omitempty"`
	VocalLanguageHint   string `json:"vocal_language_hint,omitempty"`
	InstrumentalHint    bool   `json:"instrumental_hint"`
	HasExplicitDuration bool   `json:"has_explicit_duration"`
}

type deepSeekChatRequest struct {
	Model          string                `json:"model"`
	Temperature    float64               `json:"temperature"`
	TopP           float64               `json:"top_p"`
	ResponseFormat map[string]string     `json:"response_format"`
	Messages       []deepSeekChatMessage `json:"messages"`
}

type deepSeekChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekChatResponse struct {
	Choices []struct {
		Message deepSeekChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func buildDeepSeekUserContent(input generationInput) (string, error) {
	payload := deepSeekUserInput{
		UserPrompt:          strings.TrimSpace(input.Prompt),
		UserLyrics:          strings.TrimSpace(input.Lyrics),
		VocalLanguageHint:   strings.TrimSpace(input.VocalLanguage),
		InstrumentalHint:    isInstrumentalRequest(input.Prompt) || isInstrumentalLyrics(input.Lyrics),
		HasExplicitDuration: input.DurationSet,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *aceClient) formatWithDeepSeek(input generationInput) (*deepSeekFormatResult, error) {
	if strings.TrimSpace(c.deepSeekAPIKey) == "" {
		return nil, fmt.Errorf("deepseek_api_key not configured")
	}
	if strings.TrimSpace(c.deepSeekBaseURL) == "" {
		return nil, fmt.Errorf("deepseek_base_url not configured")
	}
	if strings.TrimSpace(c.deepSeekModel) == "" {
		return nil, fmt.Errorf("deepseek_model not configured")
	}

	userContent, err := buildDeepSeekUserContent(input)
	if err != nil {
		return nil, fmt.Errorf("build deepseek user content: %w", err)
	}

	payload := deepSeekChatRequest{
		Model:       c.deepSeekModel,
		Temperature: 0.7,
		TopP:        0.9,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
		Messages: []deepSeekChatMessage{
			{Role: "system", Content: deepSeekSystemPrompt},
			{Role: "user", Content: userContent},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal deepseek request: %w", err)
	}
	log.Infof("[ace-step] deepseek chat/completions API payload: %s", compactJSONForLog(data))

	endpoint := strings.TrimRight(c.deepSeekBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build deepseek request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.deepSeekAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: deepSeekRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read deepseek response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepseek HTTP %d: %s", resp.StatusCode, string(body))
	}

	var chatResp deepSeekChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("parse deepseek response: %w (body: %s)", err, string(body))
	}
	if chatResp.Error != nil {
		return nil, fmt.Errorf("deepseek API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek response has no choices")
	}
	content := chatResp.Choices[0].Message.Content
	return parseDeepSeekFormatResult(content)
}

func parseDeepSeekFormatResult(raw string) (*deepSeekFormatResult, error) {
	raw = stripJSONFence(strings.TrimSpace(raw))
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var result deepSeekFormatResult
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid deepseek formatter JSON schema: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("invalid deepseek formatter JSON schema: multiple JSON values")
	}
	if err := validateDeepSeekFormatResult(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func stripJSONFence(raw string) string {
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	lines := strings.Split(raw, "\n")
	if len(lines) >= 3 {
		lines = lines[1 : len(lines)-1]
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return raw
}

func validateDeepSeekFormatResult(result *deepSeekFormatResult) error {
	if result == nil {
		return fmt.Errorf("deepseek formatter result is nil")
	}
	if strings.TrimSpace(result.Caption) == "" {
		return fmt.Errorf("deepseek formatter schema error: caption is required")
	}
	if result.BPM != 0 && (result.BPM < 30 || result.BPM > 300) {
		return fmt.Errorf("deepseek formatter schema error: bpm must be 30-300, got %d", result.BPM)
	}
	if result.Duration < 0 {
		return fmt.Errorf("deepseek formatter schema error: duration must be positive")
	}
	lang := strings.TrimSpace(result.VocalLanguage)
	if lang != "" && lang != "zh" && lang != "ja" && lang != "en" && lang != "unknown" {
		return fmt.Errorf("deepseek formatter schema error: unsupported vocal_language %q", lang)
	}
	if strings.TrimSpace(result.Lyrics) == "" && !result.Instrumental {
		return fmt.Errorf("deepseek formatter schema error: lyrics is required for non-instrumental output")
	}
	return nil
}

func applyDeepSeekFormatResult(input *generationInput, formatted *deepSeekFormatResult) {
	if input == nil || formatted == nil {
		return
	}
	input.Prompt = strings.TrimSpace(formatted.Caption)
	input.Lyrics = strings.TrimSpace(formatted.Lyrics)
	if formatted.Instrumental && input.Lyrics == "" {
		input.Lyrics = "[Instrumental]"
	}
	input.VocalLanguage = strings.TrimSpace(formatted.VocalLanguage)
	input.BPM = formatted.BPM
	input.Keyscale = strings.TrimSpace(formatted.KeyScale)
	input.TimeSignature = strings.TrimSpace(formatted.TimeSignature)
	input.SampleMode = false
	input.SampleQuery = ""
}
