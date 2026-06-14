package omoi

import (
	"context"
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
	text := strings.TrimSpace(ctx.ExtractPlainText())
	if text == "" || shouldSkip(text) {
		log.Infof("[omoi] skipped: text=%q", text)
		return
	}

	groupName := getGroupName(ctx)
	nickname := getNickname(ctx)

	// Get buffer snapshot for context
	buf := getBuffer(ctx.Event.GroupID)
	history := buf.Snapshot()

	trigger := BufferedMessage{
		UserID:   ctx.Event.UserID,
		Nickname: nickname,
		Text:     text,
		Time:     time.Now(),
	}

	prompt := BuildGroupMentionPrompt(groupName, history, trigger)

	bgCtx := context.Background()
	sessionID, err := getGroupSession(bgCtx, ctx.Event.GroupID, groupName)
	if err != nil {
		log.Warnf("[omoi] get session failed: %v", err)
		return
	}

	reply, err := client.SendMessage(bgCtx, sessionID, prompt)
	if err != nil {
		// If 404, try to recreate session
		if strings.Contains(err.Error(), "404") {
			_ = resetGroupSession(bgCtx, ctx.Event.GroupID)
			sessionID, err = getGroupSession(bgCtx, ctx.Event.GroupID, groupName)
			if err != nil {
				log.Warnf("[omoi] recreate session failed: %v", err)
				return
			}
			reply, err = client.SendMessage(bgCtx, sessionID, prompt)
			if err != nil {
				log.Warnf("[omoi] send message retry failed: %v", err)
				return
			}
		} else {
			log.Warnf("[omoi] send message failed: %v", err)
			return
		}
	}

	SendSplitReply(ctx, reply)
}

func handlePrivateChat(ctx *zero.Ctx) {
	ensureClient()
	if client == nil {
		return
	}
	text := strings.TrimSpace(ctx.ExtractPlainText())
	if text == "" || shouldSkip(text) {
		return
	}

	nickname := getNickname(ctx)
	prompt := BuildPrivatePrompt(nickname, ctx.Event.UserID, text)

	bgCtx := context.Background()
	sessionID, err := getPrivateSession(bgCtx, ctx.Event.UserID, nickname)
	if err != nil {
		log.Warnf("[omoi] get private session failed: %v", err)
		return
	}

	reply, err := client.SendMessage(bgCtx, sessionID, prompt)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			_ = resetPrivateSession(bgCtx, ctx.Event.UserID)
			sessionID, err = getPrivateSession(bgCtx, ctx.Event.UserID, nickname)
			if err != nil {
				log.Warnf("[omoi] recreate private session failed: %v", err)
				return
			}
			reply, err = client.SendMessage(bgCtx, sessionID, prompt)
			if err != nil {
				log.Warnf("[omoi] send private message retry failed: %v", err)
				return
			}
		} else {
			log.Warnf("[omoi] send private message failed: %v", err)
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

	text := strings.TrimSpace(ctx.ExtractPlainText())
	if text == "" || shouldSkip(text) {
		return
	}

	nickname := getNickname(ctx)
	buf := getBuffer(ctx.Event.GroupID)

	msg := BufferedMessage{
		UserID:   ctx.Event.UserID,
		Nickname: nickname,
		Text:     text,
		Time:     time.Now(),
	}

	shouldTrigger := buf.Push(msg)
	if !shouldTrigger {
		return
	}

	cfg := config.Get().Omoi

	// Probability check
	if rand.Float64() >= cfg.TriggerProbability {
		return
	}

	// Check enabled groups
	if len(cfg.EnabledGroups) > 0 {
		found := false
		for _, g := range cfg.EnabledGroups {
			if g == ctx.Event.GroupID {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}

	buf.MarkTriggered()

	// Async trigger
	go func() {
		groupName := getGroupName(ctx)
		history := buf.Snapshot()
		prompt := BuildGroupActivePrompt(groupName, history)

		bgCtx := context.Background()
		sessionID, err := getGroupSession(bgCtx, ctx.Event.GroupID, groupName)
		if err != nil {
			log.Warnf("[omoi] active trigger get session failed: %v", err)
			return
		}

		reply, err := client.SendMessage(bgCtx, sessionID, prompt)
		if err != nil {
			log.Warnf("[omoi] active trigger send failed: %v", err)
			return
		}

		SendSplitReply(ctx, reply)
	}()
}

func handleReset(ctx *zero.Ctx) {
	bgCtx := context.Background()
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
