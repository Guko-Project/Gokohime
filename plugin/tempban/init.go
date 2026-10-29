package tempban

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var (
	// bannedCommands tracks temporarily banned commands per group.
	// key: "groupID:command", value: expiry time
	bannedCommands = sync.Map{}

	silentBlockedKeywords = []string{".我朋友", "/我朋友", "!我朋友", "我朋友"}
)

func init() {
	zero.OnMessage(zero.OnlyGroup).SetPriority(1).SetBlock(false).Handle(blockDynamicallyBannedCommand)

	zero.OnCommand("啃").SetPriority(1).SetBlock(true).Handle(func(ctx *zero.Ctx) {})
	zero.OnKeywordGroup(silentBlockedKeywords).SetPriority(1).SetBlock(true).Handle(func(ctx *zero.Ctx) {})

	zero.OnCommand("ban", exactCommandRule("ban"), zero.OnlyGroup, zero.AdminPermission).
		SetBlock(true).
		Handle(handleBanCommand)

	zero.OnCommand("unban", exactCommandRule("unban"), zero.OnlyGroup, zero.AdminPermission).
		SetBlock(true).
		Handle(handleUnbanCommand)
}

// exactCommandRule adds a command boundary to ZeroBot's prefix-only matching.
func exactCommandRule(command string) zero.Rule {
	return func(ctx *zero.Ctx) bool {
		if ctx == nil || ctx.Event == nil || len(ctx.Event.Message) == 0 || ctx.Event.Message[0].Type != "text" {
			return false
		}
		text := ctx.Event.Message[0].Data["text"]
		rest, ok := strings.CutPrefix(text, zero.BotConfig.CommandPrefix+command)
		if !ok || rest == "" {
			return ok
		}
		r, _ := utf8.DecodeRuneInString(rest)
		return unicode.IsSpace(r)
	}
}

func handleBanCommand(ctx *zero.Ctx) {
	args, _ := ctx.State["args"].(string)
	parts := strings.Fields(args)
	if len(parts) < 1 {
		ctx.SendChain(message.Text("用法: .ban <命令> [时长(分钟，默认30)]"))
		return
	}

	cmd := normalizeCommandName(parts[0])
	if cmd == "" {
		ctx.SendChain(message.Text("用法: .ban <命令> [时长(分钟，默认30)]"))
		return
	}

	minutes := 30
	if len(parts) >= 2 {
		if parsed, err := strconv.Atoi(parts[1]); err == nil && parsed > 0 {
			minutes = parsed
		}
	}

	key := banKey(ctx.Event.GroupID, cmd)
	bannedCommands.Store(key, time.Now().Add(time.Duration(minutes)*time.Minute))
	ctx.SendChain(message.Text(fmt.Sprintf("命令 %s 已在本群禁用 %d 分钟", cmd, minutes)))
}

func handleUnbanCommand(ctx *zero.Ctx) {
	args, _ := ctx.State["args"].(string)
	cmd := normalizeCommandName(args)
	if cmd == "" {
		ctx.SendChain(message.Text("用法: .unban <命令>"))
		return
	}

	bannedCommands.Delete(banKey(ctx.Event.GroupID, cmd))
	ctx.SendChain(message.Text(fmt.Sprintf("命令 %s 已解禁", cmd)))
}

func blockDynamicallyBannedCommand(ctx *zero.Ctx) {
	if ctx == nil || ctx.Event == nil {
		return
	}

	cmd, ok := extractConfiguredCommandName(ctx.Event.RawMessage)
	if !ok || cmd == "ban" || cmd == "unban" {
		return
	}
	if IsBanned(ctx.Event.GroupID, cmd) {
		ctx.Block()
	}
}

func extractConfiguredCommandName(raw string) (string, bool) {
	prefix := "."
	if cfg := config.Get(); cfg != nil && strings.TrimSpace(cfg.Bot.CommandPrefix) != "" {
		prefix = strings.TrimSpace(cfg.Bot.CommandPrefix)
	}
	return extractCommandName(raw, prefix)
}

func extractCommandName(raw, prefix string) (string, bool) {
	raw = strings.TrimSpace(raw)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || !strings.HasPrefix(raw, prefix) {
		return "", false
	}

	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}

	cmd := normalizeCommandName(fields[0])
	if cmd == "" {
		return "", false
	}
	return cmd, true
}

func normalizeCommandName(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.TrimLeft(input, "./!")
	return strings.TrimSpace(input)
}

func banKey(groupID int64, cmd string) string {
	return fmt.Sprintf("%d:%s", groupID, normalizeCommandName(cmd))
}

// IsBanned checks if a command is banned in a group.
func IsBanned(groupID int64, cmd string) bool {
	key := banKey(groupID, cmd)
	val, ok := bannedCommands.Load(key)
	if !ok {
		return false
	}
	expiry := val.(time.Time)
	if time.Now().After(expiry) {
		bannedCommands.Delete(key)
		return false
	}
	return true
}
