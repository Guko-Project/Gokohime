package omoi

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func init() {
	rand.Seed(time.Now().UnixNano())

	// @bot group chat — always reply (unless command prefix)
	zero.OnMessage(zero.OnlyToMe, zero.OnlyGroup).
		SetBlock(true).SetPriority(50).Handle(handleGroupMention)

	// Private chat — always reply (unless command prefix)
	zero.OnMessage(zero.OnlyPrivate).
		SetBlock(true).SetPriority(50).Handle(handlePrivateChat)

	// Group message observer — buffer + active trigger (non-blocking, low priority)
	zero.OnMessage(zero.OnlyGroup).
		SetPriority(99).Handle(handleGroupObserve)

	// .chat-reset command
	zero.OnCommand("chat-reset").SetBlock(true).Handle(handleReset)
}

var clientOnce sync.Once

// ensureClient lazily initializes the Omoi client (config is loaded in main).
func ensureClient() {
	clientOnce.Do(func() {
		initClient()
	})
}

// shouldSkip checks if the message text starts with any skip prefix.
func shouldSkip(text string) bool {
	cfg := config.Get()
	if cfg == nil {
		return false
	}
	trimmed := strings.TrimSpace(text)
	for _, prefix := range cfg.Omoi.SkipPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// getNickname extracts the user's display name.
func getNickname(ctx *zero.Ctx) string {
	if ctx.Event.Sender != nil {
		if ctx.Event.Sender.Card != "" {
			return ctx.Event.Sender.Card
		}
		if ctx.Event.Sender.NickName != "" {
			return ctx.Event.Sender.NickName
		}
	}
	return strconv.FormatInt(ctx.Event.UserID, 10)
}

// getGroupName extracts the group name.
func getGroupName(ctx *zero.Ctx) string {
	info := ctx.GetGroupInfo(ctx.Event.GroupID, false)
	if info.Name != "" {
		return info.Name
	}
	return strconv.FormatInt(ctx.Event.GroupID, 10)
}

func handleGroupMention(ctx *zero.Ctx) {
	log.Infof("[omoi] handleGroupMention triggered, group=%d user=%d", ctx.Event.GroupID, ctx.Event.UserID)
	ensureClient()
	if client == nil {
		log.Warn("[omoi] client is nil after ensureClient")
		return
	}
	refs := extractMedia(ctx, true)
	text := strings.TrimSpace(ctx.ExtractPlainText())
	if shouldSkip(text) {
		log.Infof("[omoi] skipped: text=%q", text)
		return
	}

	groupName := getGroupName(ctx)
	nickname := getNickname(ctx)

	// Get buffer snapshot for context
	buf := getBuffer(ctx.Event.GroupID)
	history := buf.Snapshot()
	attachments := selectMedia(refs, history)

	trigger := BufferedMessage{
		UserID:   ctx.Event.UserID,
		Nickname: nickname,
		Text:     describeMedia(text, refs),
		Media:    refs,
		Time:     time.Now(),
	}

	prompt := BuildGroupMentionPrompt(groupName, history, trigger)

	bgCtx := channelRequest(requestContext(), ctx.Event, false)
	reply, err := sendGroupMessage(bgCtx, ctx.Event.GroupID, groupName, prompt, attachments...)
	if err != nil {
		log.Warnf("[omoi] send message failed: %v", err)
		reportFailure(ctx, bgCtx, err)
		return
	}
	// A bare mention is a request for acknowledgement, even if the model stays silent.
	cfg := config.Get().Omoi
	if text == "" && len(refs) == 0 && (strings.TrimSpace(reply) == "" || (cfg.SkipMarker != "" && strings.Contains(reply, cfg.SkipMarker))) {
		reply = "我在，怎么啦？"
	}

	SendSplitReply(ctx, reply)
}

func handlePrivateChat(ctx *zero.Ctx) {
	ensureClient()
	if client == nil {
		return
	}
	refs := extractMedia(ctx, true)
	text := strings.TrimSpace(ctx.ExtractPlainText())
	if (text == "" && len(refs) == 0) || shouldSkip(text) {
		return
	}

	nickname := getNickname(ctx)
	attachments := refs
	prompt := BuildPrivatePrompt(nickname, ctx.Event.UserID, describeMedia(text, refs))

	bgCtx := channelRequest(requestContext(), ctx.Event, false)
	sessionID, err := getPrivateSession(bgCtx, ctx.Event.UserID, nickname)
	if err != nil {
		log.Warnf("[omoi] get private session failed: %v", err)
		return
	}

	reply, err := client.SendMessage(bgCtx, sessionID, prompt, attachments...)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			_ = resetPrivateSession(bgCtx, ctx.Event.UserID)
			sessionID, err = getPrivateSession(bgCtx, ctx.Event.UserID, nickname)
			if err != nil {
				log.Warnf("[omoi] recreate private session failed: %v", err)
				return
			}
			reply, err = client.SendMessage(bgCtx, sessionID, prompt, attachments...)
			if err != nil {
				log.Warnf("[omoi] send private message retry failed: %v", err)
				reportFailure(ctx, bgCtx, err)
				return
			}
		} else {
			log.Warnf("[omoi] send private message failed: %v", err)
			reportFailure(ctx, bgCtx, err)
			return
		}
	}

	SendSplitReply(ctx, reply)
}

func handleGroupObserve(ctx *zero.Ctx) {
	log.Infof("[omoi] handleGroupObserve triggered, group=%d user=%d isToMe=%v", ctx.Event.GroupID, ctx.Event.UserID, ctx.Event.IsToMe)
	ensureClient()
	if client == nil {
		log.Warn("[omoi] observe: client is nil")
		return
	}
	// Skip self messages
	if ctx.Event.UserID != 0 && ctx.Event.UserID == ctx.Event.SelfID {
		return
	}

	refs := extractMedia(ctx, false)
	text := strings.TrimSpace(ctx.ExtractPlainText())
	if (text == "" && len(refs) == 0) || shouldSkip(text) {
		return
	}

	nickname := getNickname(ctx)
	buf := getBuffer(ctx.Event.GroupID)

	msg := BufferedMessage{
		UserID:   ctx.Event.UserID,
		Nickname: nickname,
		Text:     describeMedia(text, refs),
		Media:    refs,
		Time:     time.Now(),
	}

	if !buf.Push(msg, ctx.Event.GroupID) {
		return
	}
	history := buf.Snapshot()
	attachments := selectMedia(nil, history)

	// Async trigger
	go func() {
		groupName := getGroupName(ctx)
		prompt := BuildGroupActivePrompt(groupName, history, config.Get().Omoi.SkipMarker)

		bgCtx := channelRequest(requestContext(), ctx.Event, true)
		reply, err := sendGroupMessage(bgCtx, ctx.Event.GroupID, groupName, prompt, attachments...)
		if err != nil {
			log.Warnf("[omoi] active trigger send failed: %v", err)
			reportFailure(ctx, bgCtx, err)
			return
		}

		SendSplitReply(ctx, reply)
	}()
}

func handleReset(ctx *zero.Ctx) {
	ensureClient()
	if ctx.Event.GroupID != 0 {
		permitted := ctx.Event.Sender != nil && (ctx.Event.Sender.Role == "owner" || ctx.Event.Sender.Role == "admin")
		for _, id := range config.Get().Bot.SuperUsers {
			if id == ctx.Event.UserID {
				permitted = true
			}
		}
		if !permitted {
			ctx.Send("群会话重置会取消全部定时任务，请由群主或管理员操作。自己的任务可以通过对话取消。")
			return
		}
	}

	bgCtx := channelRequest(requestContext(), ctx.Event, false)
	var err error

	if ctx.Event.GroupID != 0 {
		err = resetGroupSession(bgCtx, ctx.Event.GroupID)
	} else {
		err = resetPrivateSession(bgCtx, ctx.Event.UserID)
	}

	if err != nil {
		ctx.Send(fmt.Sprintf("会话重置失败: %v", err))
		return
	}
	ctx.Send("会话已重置，下次对话将开启新会话")
}
