// Package acestep ACE-Step 1.5 AI音乐生成
package acestep

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var (
	client     *aceClient
	clientOnce sync.Once
)

func ensureClient() {
	clientOnce.Do(func() {
		cfg := config.Get()
		if cfg == nil {
			log.Warn("[ace-step] config not loaded yet")
			return
		}
		ac := cfg.AceStep
		if ac.APIKey == "" {
			log.Warn("[ace-step] api_key not configured")
			return
		}
		baseURL := ac.BaseURL
		if baseURL == "" {
			baseURL = "https://colasama--ace-step-api-serve.modal.run"
		}
		client = newClientWithConfig(baseURL, ac.APIKey, ac.UseDeepSeekLM, ac.DeepSeekAPIKey, ac.DeepSeekBaseURL, ac.DeepSeekModel)
		log.Infof("[ace-step] client initialized: use_deepseek_lm=%v deepseek_base_url=%s deepseek_model=%s", ac.UseDeepSeekLM, ac.DeepSeekBaseURL, ac.DeepSeekModel)
	})
}

func init() {
	zero.OnCommand("ace").SetBlock(true).SetPriority(30).Handle(handleAce)
}

func handleAce(ctx *zero.Ctx) {
	ensureClient()
	if client == nil {
		ctx.SendChain(message.Text("❌ ACE-Step 未配置，请联系管理员"))
		return
	}

	cfg := config.Get().AceStep
	args := strings.TrimSpace(ctx.State["args"].(string))
	if args == "" {
		ctx.SendChain(message.Text("正在锐意开发中！\n用法：\n.ace <想要制作的音乐风格和内容，如果要带歌词请明确指示需要歌词和演唱语言风格等描述> [时长秒数]\n.ace <音乐风格描述> | <歌词> [时长秒数]\n如果不填写秒数，ACE-Step 会自动推断合适时长。\n推荐先阅读 prompt 编写教程：https://github.com/ace-step/ACE-Step-1.5/blob/main/docs/zh/Tutorial.md\n例：.ace 日本VOCALOID摇滚 | 我不想上班，我真的不想上班 30"))
		return
	}

	input := parseGenerationInput(args, cfg.DefaultDuration, cfg.MaxDuration)

	ctx.SendChain(message.Text("ヽ(ﾟ∀ﾟ)ﾒ(ﾟ∀ﾟ)ﾉ 收到！马上为你谱写一首美丽的乐曲——"))
	if client.useDeepSeekLM {
		log.Infof("[ace-step] formatting with DeepSeek: prompt=%s lyrics_len=%d explicit_duration=%v", input.Prompt, len(input.Lyrics), input.DurationSet)
		formatted, err := client.formatWithDeepSeek(input)
		if err != nil {
			log.Warnf("[ace-step] deepseek format failed: %v", err)
			ctx.SendChain(message.Text("❌ DeepSeek 歌曲参数生成失败：" + err.Error()))
			return
		}
		applyDeepSeekFormatResult(&input, formatted)
		input.Duration = resolveDuration(input, formatted, cfg.DefaultDuration)
		log.Infof("[ace-step] deepseek formatted: duration=%.1f language=%s instrumental=%v lyrics_len=%d", input.Duration, input.VocalLanguage, formatted.Instrumental, len(input.Lyrics))
	} else if input.Lyrics != "" {
		log.Infof("[ace-step] formatting explicit lyrics: prompt=%s lyrics_len=%d", input.Prompt, len(input.Lyrics))
		formatted, err := client.formatInput(input.Prompt, input.Lyrics, input.VocalLanguage)
		if err != nil {
			log.Warnf("[ace-step] format input failed: %v", err)
			ctx.SendChain(message.Text("❌ 歌词格式化失败：" + err.Error()))
			return
		}
		applySampleData(&input, formatted, !input.DurationSet)
		log.Infof("[ace-step] lyrics formatted: duration=%.1f language=%s lyrics_len=%d", input.Duration, input.VocalLanguage, len(input.Lyrics))
	} else {
		formatted, err := client.createSample(buildSampleQuery(input.Prompt))
		if err != nil {
			log.Warnf("[ace-step] create sample failed: %v", err)
			ctx.SendChain(message.Text("❌ 歌曲参数生成失败：" + err.Error()))
			return
		}
		applySampleData(&input, formatted, !input.DurationSet)
		log.Infof("[ace-step] sample formatted: duration=%.1f language=%s lyrics_len=%d", input.Duration, input.VocalLanguage, len(input.Lyrics))
	}
	input.SampleMode = false
	input.SampleQuery = ""
	if input.Lyrics == "" {
		input.Lyrics = "[Instrumental]"
	}
	if input.VocalLanguage == "" {
		input.VocalLanguage = "unknown"
	}
	if input.Duration <= 0 {
		input.Duration = float64(cfg.DefaultDuration)
	}

	// Submit task
	taskID, err := client.submitTask(input)
	if err != nil {
		log.Warnf("[ace-step] submit failed: %v", err)
		ctx.SendChain(message.Text("❌ 提交任务失败：" + err.Error()))
		return
	}
	log.Infof("[ace-step] task submitted: %s", taskID)

	// Poll result
	pollInterval := time.Duration(cfg.PollIntervalSec) * time.Second
	pollTimeout := time.Duration(cfg.PollTimeoutSec) * time.Second
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}
	if pollTimeout <= 0 {
		pollTimeout = 120 * time.Second
	}

	audioURL, err := client.pollResult(taskID, pollInterval, pollTimeout)
	if err != nil {
		log.Warnf("[ace-step] poll failed: %v", err)
		ctx.SendChain(message.Text("❌ 生成失败：" + err.Error()))
		return
	}

	// Download to persistent local file under the docker-mounted data directory.
	audioDir := "/app/data/ace-step/audio"
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		log.Warnf("[ace-step] create audio dir failed: %v", err)
		ctx.SendChain(message.Text("❌ 创建音频保存目录失败：" + err.Error()))
		return
	}
	requester := sanitizeFilenamePart(getRequesterName(ctx))
	timestamp := time.Now().Format("20060102_150405")
	localPath := fmt.Sprintf("%s/%s_%s.mp3", audioDir, requester, timestamp)
	if err := client.downloadAudio(audioURL, localPath); err != nil {
		log.Warnf("[ace-step] download failed: %v", err)
		ctx.SendChain(message.Text("❌ 下载音频失败：" + err.Error()))
		return
	}
	log.Infof("[ace-step] audio saved: %s", localPath)

	log.Infof("[ace-step] sending record: %s", localPath)
	segment, err := recordSegmentFromFile(localPath)
	if err != nil {
		log.Warnf("[ace-step] build record failed: %v", err)
		ctx.SendChain(message.Text("❌ 音频发送失败：" + err.Error()))
		return
	}
	ctx.SendChain(segment)
}

type generationInput struct {
	Prompt        string
	Lyrics        string
	VocalLanguage string
	SampleMode    bool
	SampleQuery   string
	BPM           int
	Keyscale      string
	TimeSignature string
	Duration      float64
	DurationSet   bool
}

func applySampleData(input *generationInput, sample *sampleData, applyDuration bool) {
	if sample == nil {
		return
	}
	if caption := strings.TrimSpace(sample.Caption); caption != "" {
		input.Prompt = caption
	}
	if lyrics := strings.TrimSpace(sample.Lyrics); lyrics != "" {
		input.Lyrics = lyrics
	}
	if language := strings.TrimSpace(sample.VocalLanguage); language != "" {
		input.VocalLanguage = language
	}
	input.BPM = sample.BPM
	input.Keyscale = sample.normalizedKeyscale()
	input.TimeSignature = sample.normalizedTimeSignature()
	if applyDuration && sample.Duration > 0 {
		input.Duration = sample.Duration
	}
}

func parseGenerationInput(args string, defaultDur, maxDur int) generationInput {
	withoutDuration, duration, durationSet := parseArgsWithDuration(args, defaultDur, maxDur)
	input := generationInput{Prompt: withoutDuration, Duration: duration, DurationSet: durationSet}

	parts := strings.SplitN(withoutDuration, "|", 2)
	if len(parts) == 2 {
		input.Prompt = strings.TrimSpace(parts[0])
		input.Lyrics = strings.TrimSpace(parts[1])
		input.VocalLanguage = detectVocalLanguage(input.Lyrics)
		if !durationSet {
			input.Duration = -1
		}
		if input.Prompt == "" {
			input.Prompt = strings.TrimSpace(args)
		}
	}
	return input
}

func buildSampleQuery(prompt string) string {
	if isInstrumentalRequest(prompt) {
		return prompt
	}
	return prompt + "\nGenerate sung vocals with complete lyrics. Do not make this instrumental."
}

func isInstrumentalRequest(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "instrumental") || strings.Contains(prompt, "纯音乐") || strings.Contains(prompt, "无歌词")
}

func detectVocalLanguage(text string) string {
	for _, r := range text {
		switch {
		case r >= '\u3040' && r <= '\u30ff':
			return "ja"
		case r >= '\u4e00' && r <= '\u9fff':
			return "zh"
		}
	}
	return "unknown"
}

func recordSegmentFromFile(path string) (message.Segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return message.Segment{}, fmt.Errorf("read audio file: %w", err)
	}
	return message.Record("base64://" + base64.StdEncoding.EncodeToString(data)), nil
}

func getRequesterName(ctx *zero.Ctx) string {
	if ctx != nil && ctx.Event != nil && ctx.Event.Sender != nil {
		if card := strings.TrimSpace(ctx.Event.Sender.Card); card != "" {
			return card
		}
		if nick := strings.TrimSpace(ctx.Event.Sender.NickName); nick != "" {
			return nick
		}
	}
	if ctx != nil && ctx.Event != nil && ctx.Event.UserID != 0 {
		return strconv.FormatInt(ctx.Event.UserID, 10)
	}
	return "unknown"
}

func sanitizeFilenamePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}

	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('_')
		case '\n', '\r', '\t':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}

	cleaned := strings.Trim(b.String(), " ._")
	if cleaned == "" {
		return "unknown"
	}
	if len([]rune(cleaned)) > 40 {
		cleaned = string([]rune(cleaned)[:40])
	}
	return cleaned
}

// parseArgs extracts prompt and duration from the command args.
// If the last token is a positive number, it's used as duration.
func parseArgs(args string, defaultDur, maxDur int) (string, float64) {
	prompt, duration, _ := parseArgsWithDuration(args, defaultDur, maxDur)
	return prompt, duration
}

func parseArgsWithDuration(args string, defaultDur, maxDur int) (string, float64, bool) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return args, float64(defaultDur), false
	}

	last := parts[len(parts)-1]
	if dur, err := strconv.Atoi(last); err == nil && dur > 0 {
		prompt := strings.TrimSpace(strings.Join(parts[:len(parts)-1], " "))
		if prompt == "" {
			return args, float64(defaultDur), false
		}
		return prompt, float64(dur), true
	}

	return args, float64(defaultDur), false
}
