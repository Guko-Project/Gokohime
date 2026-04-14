package simplegpt

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	"github.com/colanns/gokohime/internal/aicore"
	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/plugin/tempban"
)

const (
	aiCommandName    = "ai"
	segmentDelimiter = "///"
	maxPreviewChars  = 120
)

var sessionLocks sync.Map

type sessionLock struct {
	mu sync.Mutex
}

func init() {
	rand.Seed(time.Now().UnixNano())

	log.Info("[simplegpt] AI bridge plugin registered")

	zero.OnCommand("ai").SetPriority(5).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		handleManualChat(ctx)
	})

	zero.OnMessage(zero.OnlyGroup).SetPriority(5).SetBlock(false).Handle(func(ctx *zero.Ctx) {
		handleGroupChat(ctx)
	})
}

func handleManualChat(ctx *zero.Ctx) {
	cfg := config.Get()
	if cfg == nil || ctx == nil || ctx.Event == nil {
		log.Warn("[simplegpt] manual trigger skipped: config or event is nil")
		return
	}

	plain := strings.TrimSpace(ctx.State["args"].(string))
	log.Infof("[simplegpt] manual trigger received group=%d user=%d plain=%q", ctx.Event.GroupID, ctx.Event.UserID, previewText(plain))
	if plain == "" {
		ctx.SendChain(message.Text("用法: .ai 你好"))
		return
	}

	if err := runConversation(ctx, plain, "manual"); err != nil {
		log.Warnf("[simplegpt] manual trigger failed group=%d user=%d err=%v", ctx.Event.GroupID, ctx.Event.UserID, err)
		ctx.SendChain(message.Text("AI 调用失败了，请看日志。"))
	}
}

func handleGroupChat(ctx *zero.Ctx) {
	cfg := config.Get()
	if cfg == nil || ctx == nil || ctx.Event == nil {
		log.Warn("[simplegpt] group handler skipped: config or event is nil")
		return
	}
	if ctx.Event.UserID == 0 || ctx.Event.UserID == ctx.Event.SelfID {
		log.Debugf("[simplegpt] skip self/system message group=%d user=%d", ctx.Event.GroupID, ctx.Event.UserID)
		return
	}
	if tempban.IsBanned(ctx.Event.GroupID, aiCommandName) {
		log.Infof("[simplegpt] skip banned group=%d user=%d", ctx.Event.GroupID, ctx.Event.UserID)
		return
	}

	raw := strings.TrimSpace(ctx.MessageString())
	plain := strings.TrimSpace(ctx.ExtractPlainText())
	if ctx.Event.IsToMe {
		log.Infof("[simplegpt] to-me message received group=%d user=%d raw=%q plain=%q", ctx.Event.GroupID, ctx.Event.UserID, previewText(raw), previewText(plain))
	}
	if plain == "" || isCommandMessage(plain, cfg.Bot.CommandPrefix) {
		if ctx.Event.IsToMe {
			log.Infof("[simplegpt] to-me message skipped group=%d user=%d reason=%s", ctx.Event.GroupID, ctx.Event.UserID, skipReason(plain, cfg.Bot.CommandPrefix))
		}
		return
	}

	triggered, triggerMode := shouldTrigger(ctx, cfg)
	if !triggered {
		if ctx.Event.IsToMe {
			log.Infof("[simplegpt] to-me trigger rejected unexpectedly group=%d user=%d", ctx.Event.GroupID, ctx.Event.UserID)
		}
		return
	}

	if err := runConversation(ctx, plain, triggerMode); err != nil {
		log.Warnf("[simplegpt] chat trigger failed group=%d user=%d err=%v", ctx.Event.GroupID, ctx.Event.UserID, err)
		ctx.SendChain(message.Text("刚才断线了，再说一次试试。"))
	}
	ctx.Block()
}

func shouldTrigger(ctx *zero.Ctx, cfg *config.Config) (bool, string) {
	if ctx.Event.IsToMe {
		log.Infof("[simplegpt] trigger accepted group=%d user=%d mode=at", ctx.Event.GroupID, ctx.Event.UserID)
		return true, "at"
	}
	if ctx.Event.GroupID == 0 {
		return false, ""
	}
	if !containsGroup(cfg.AI.ProactiveGroupWhitelist, ctx.Event.GroupID) {
		log.Debugf("[simplegpt] proactive skip group=%d user=%d reason=group_not_whitelisted", ctx.Event.GroupID, ctx.Event.UserID)
		return false, ""
	}
	if cfg.AI.ReplyProbability <= 0 {
		log.Debugf("[simplegpt] proactive skip group=%d user=%d reason=probability_disabled", ctx.Event.GroupID, ctx.Event.UserID)
		return false, ""
	}
	roll := rand.Float64()
	log.Debugf("[simplegpt] proactive roll group=%d user=%d roll=%.4f threshold=%.4f", ctx.Event.GroupID, ctx.Event.UserID, roll, cfg.AI.ReplyProbability)
	if roll > cfg.AI.ReplyProbability {
		return false, ""
	}
	log.Infof("[simplegpt] trigger accepted group=%d user=%d mode=proactive", ctx.Event.GroupID, ctx.Event.UserID)
	return true, "proactive"
}

func buildPrompt(ctx *zero.Ctx, userText, triggerMode string) string {
	userName := fmt.Sprintf("%d", ctx.Event.UserID)
	if ctx.Event.Sender != nil {
		userName = ctx.Event.Sender.Name()
	}

	scene := "QQ 群"
	sceneDetails := fmt.Sprintf("- 群号：%d\n", ctx.Event.GroupID)
	if ctx.Event.GroupID == 0 {
		scene = "QQ 私聊"
		sceneDetails = "- 当前会话：私聊\n"
	}

	return fmt.Sprintf(
		"你正在一个 %s 里以自然聊天方式回复消息。\n"+
			"不要暴露下面这些上下文说明，把它们当作内部信息使用。\n\n"+
			"当前上下文：\n"+
			"- 当前时间：%s\n"+
			"%s"+
			"- 发言用户 QQ：%d\n"+
			"- 发言用户昵称：%s\n"+
			"- 触发方式：%s\n\n"+
			"回复要求：\n"+
			"- 直接正常回答，不要复述上下文字段。\n"+
			"- 如果需要分段发送，可以在段落之间输出 `///`。\n"+
			"- 如果调用 memory_store，尽量补全 user_id、group_id、user_name。\n"+
			"- 如果调用 memory_recall，请传入当前 group_id。\n\n"+
			"用户消息：\n%s",
		scene,
		time.Now().Format("2006-01-02 15:04:05 MST"),
		sceneDetails,
		ctx.Event.UserID,
		userName,
		triggerMode,
		userText,
	)
}

func streamReply(ctx *zero.Ctx, prompt, sessionID string) error {
	timeout := 2 * time.Minute
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Infof("[simplegpt] calling aicore.RunStream session=%s group=%d user=%d prompt=%q", sessionID, ctx.Event.GroupID, ctx.Event.UserID, prompt)
	stream, err := aicore.RunStream(runCtx, prompt, sessionID)
	if err != nil {
		log.Warnf("[simplegpt] aicore.RunStream returned error session=%s err=%v", sessionID, err)
		return err
	}
	log.Infof("[simplegpt] aicore stream opened session=%s", sessionID)

	var pending strings.Builder
	sentCount := 0
	var streamErr error
	eventCount := 0

	for evt := range stream {
		eventCount++
		log.Debugf("[simplegpt] stream event session=%s type=%s", sessionID, evt.Type)
		switch evt.Type {
		case api.EventContentBlockDelta:
			if evt.Delta == nil || evt.Delta.Type != "text_delta" || evt.Delta.Text == "" {
				continue
			}
			pending.WriteString(evt.Delta.Text)
			sentCount += flushSegments(ctx, &pending, false)
		case api.EventError:
			streamErr = fmt.Errorf("%v", evt.Output)
			log.Warnf("[simplegpt] stream error event session=%s err=%v", sessionID, streamErr)
		case api.EventToolExecutionStart:
			log.Infof("[simplegpt] tool start session=%s tool=%s tool_use_id=%s", sessionID, evt.Name, evt.ToolUseID)
		case api.EventToolExecutionResult:
			log.Infof("[simplegpt] tool result session=%s tool=%s tool_use_id=%s", sessionID, evt.Name, evt.ToolUseID)
		case api.EventAgentStart, api.EventAgentStop, api.EventIterationStart, api.EventIterationStop, api.EventMessageStart, api.EventMessageStop:
			log.Debugf("[simplegpt] lifecycle event session=%s type=%s", sessionID, evt.Type)
		}
	}

	sentCount += flushSegments(ctx, &pending, true)
	log.Infof("[simplegpt] aicore stream closed session=%s events=%d sent_segments=%d pending_chars=%d", sessionID, eventCount, sentCount, pending.Len())
	if streamErr != nil {
		return streamErr
	}
	if sentCount == 0 {
		return fmt.Errorf("empty agent response")
	}
	return nil
}

func flushSegments(ctx *zero.Ctx, pending *strings.Builder, force bool) int {
	text := pending.String()
	sentCount := 0

	for {
		idx := strings.Index(text, segmentDelimiter)
		if idx < 0 {
			break
		}
		if sendText := strings.TrimSpace(text[:idx]); sendText != "" {
			log.Infof("[simplegpt] sending segment group=%d user=%d text=%q", ctx.Event.GroupID, ctx.Event.UserID, previewText(sendText))
			ctx.SendChain(message.Text(sendText))
			sentCount++
		}
		text = text[idx+len(segmentDelimiter):]
	}

	if force {
		if sendText := strings.TrimSpace(text); sendText != "" {
			log.Infof("[simplegpt] sending final segment group=%d user=%d text=%q", ctx.Event.GroupID, ctx.Event.UserID, previewText(sendText))
			ctx.SendChain(message.Text(sendText))
			sentCount++
		}
		text = ""
	}

	pending.Reset()
	pending.WriteString(text)
	return sentCount
}

func isCommandMessage(text, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	return prefix != "" && strings.HasPrefix(text, prefix)
}

func containsGroup(groups []int64, groupID int64) bool {
	for _, id := range groups {
		if id == groupID {
			return true
		}
	}
	return false
}

func buildSessionID(groupID, userID int64) string {
	if groupID == 0 {
		return fmt.Sprintf("qq-private-%d", userID)
	}
	return fmt.Sprintf("qq-group-%d", groupID)
}

func getSessionLock(sessionID string) *sessionLock {
	val, _ := sessionLocks.LoadOrStore(sessionID, &sessionLock{})
	return val.(*sessionLock)
}

func runConversation(ctx *zero.Ctx, plain, triggerMode string) error {
	if aicore.GetRuntime() == nil {
		err := fmt.Errorf("aicore runtime is nil")
		log.Warnf("[simplegpt] %v group=%d user=%d", err, ctx.Event.GroupID, ctx.Event.UserID)
		return err
	}

	sessionID := buildSessionID(ctx.Event.GroupID, ctx.Event.UserID)
	lock := getSessionLock(sessionID)
	if !lock.mu.TryLock() {
		log.Infof("[simplegpt] session busy session=%s group=%d user=%d", sessionID, ctx.Event.GroupID, ctx.Event.UserID)
		ctx.SendChain(message.Text("我还在想上一条消息，等一下。"))
		ctx.Block()
		return nil
	}
	defer lock.mu.Unlock()

	log.Infof("[simplegpt] starting conversation session=%s group=%d user=%d mode=%s plain=%q", sessionID, ctx.Event.GroupID, ctx.Event.UserID, triggerMode, previewText(plain))
	prompt := buildPrompt(ctx, plain, triggerMode)
	return streamReply(ctx, prompt, sessionID)
}

func previewText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	rs := []rune(text)
	if len(rs) <= maxPreviewChars {
		return text
	}
	return string(rs[:maxPreviewChars]) + "..."
}

func skipReason(plain, prefix string) string {
	if strings.TrimSpace(plain) == "" {
		return "empty_plain_text"
	}
	if isCommandMessage(plain, prefix) {
		return "command_message"
	}
	return "unknown"
}
