package sing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type scoreValidationError struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

func (e *scoreValidationError) Error() string {
	return fmt.Sprintf("乐谱内容第 %d 行：%s", e.Line, e.Message)
}
func (e *scoreValidationError) Unwrap() error { return httpError(422) }

// Keep the first newline: an empty command line means the default singer.
func splitScoreArgs(args string) (singer, text string) {
	args = strings.ReplaceAll(args, "\r\n", "\n")
	first, rest, _ := strings.Cut(args, "\n")
	return strings.TrimSpace(first), rest
}

func scoreVoice(name string, catalog voiceCatalog) (voice, error) {
	if name == "" {
		name = defaultVoiceID
	}
	for _, v := range catalog.Voices {
		for _, alias := range append([]string{v.ID, v.Name}, v.Aliases...) {
			if strings.EqualFold(name, alias) {
				if !v.Available {
					return v, errVoiceUnavailable
				}
				return v, nil
			}
		}
	}
	return voice{}, errScoreSinger
}

var errScoreSinger = errors.New("unknown score singer")

func handleScore(ctx *zero.Ctx) {
	// ZeroBot trims args; read original text to preserve .score\n without a singer.
	var raw strings.Builder
	for _, seg := range ctx.Event.Message {
		if seg.Type == "text" {
			raw.WriteString(seg.Data["text"])
		}
	}
	args := strings.TrimPrefix(raw.String(), zero.BotConfig.CommandPrefix+"score")
	singer, text := splitScoreArgs(args)
	if strings.TrimSpace(text) == "" {
		ctx.SendChain(message.ImageBytes(scoreHelpImage))
		return
	}
	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.Sing.APIKey) == "" {
		ctx.SendChain(message.Text("唱歌功能还没有配置好，请联系管理员。"))
		return
	}
	target := destination{ctx.Event.SelfID, ctx.Event.GroupID, ctx.Event.UserID}
	if _, loaded := pending.LoadOrStore(target, struct{}{}); loaded {
		ctx.SendChain(message.Text("上一首还在准备中，等我唱完再点吧！"))
		return
	}
	settings := cfg.Sing
	go func() {
		defer pending.Delete(target)
		if err := runScore(target, singer, text, settings); err != nil {
			log.Warnf("[score] request failed: %v", err)
			reply := "这份乐谱暂时没能唱出来，请稍后再试！"
			var validation *scoreValidationError
			switch {
			case errors.As(err, &validation):
				reply = validation.Error()
			case errors.Is(err, errScoreSinger):
				reply = "没有找到这个歌手，请用 .voices 查看音源和别名。"
			case errors.Is(err, errVoiceUnavailable), errors.Is(err, httpError(503)):
				reply = "所选音源暂不可用，请用 .voices 查看音源列表。"
			case errors.Is(err, httpError(422)):
				reply = "乐谱格式有误或内容过长，请检查；发送 .score 查看示例。"
			case errors.Is(err, context.DeadlineExceeded):
				reply = "乐谱合成等待超时，请稍后再试！"
			}
			if detail, ok := generationReply(err, "自填乐谱"); ok {
				reply = detail
			}
			target.send(message.Text(reply))
		}
	}()
}

func runScore(target destination, singer, text string, cfg config.SingConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := newClient(cfg.BaseURL, cfg.APIKey, "score")
	catalog, err := c.voices(ctx)
	if err != nil {
		return err
	}
	v, err := scoreVoice(singer, catalog)
	if err != nil {
		return err
	}
	c.voiceID = v.ID
	body := map[string]string{"text": text, "voice_id": v.ID}
	var parsed struct {
		Duration float64 `json:"duration_seconds"`
	}
	if err := c.json(ctx, http.MethodPost, "/parse_score", body, &parsed); err != nil {
		return err
	}
	if parsed.Duration <= 0 || parsed.Duration > 1800 || math.IsNaN(parsed.Duration) || math.IsInf(parsed.Duration, 0) {
		return errors.New("invalid score duration")
	}
	timeout := time.Duration(cfg.PollTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	if parsed.Duration > 32 {
		timeout += time.Duration(parsed.Duration*60) * time.Second
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var task struct {
		ID string `json:"task_id"`
	}
	if err := c.json(ctx, http.MethodPost, "/release_score_task", body, &task); err != nil {
		return err
	}
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("DiffSinger returned an empty task ID")
	}
	target.send(message.Text(warmups[rand.IntN(len(warmups))]))
	log.Infof("[score] submitted task=%s", task.ID)
	return deliverSong(ctx, target, c, task.ID, time.Duration(cfg.PollIntervalSec)*time.Second)
}
