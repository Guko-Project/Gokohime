package commandlog

import (
	"context"
	"os"
	"strings"

	"github.com/colanns/gokohime/internal/commandutil"
	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
)

var (
	knownPrefixedCommands = map[string]struct{}{
		"add":        {},
		"ai":         {},
		"banana":     {},
		"banana-pro": {},
		"ban":        {},
		"cg":         {},
		"chp":        {},
		"cp":         {},
		"eat":        {},
		"eadd":       {},
		"eatwhat":    {},
		"eats":       {},
		"edel":       {},
		"help":       {},
		"jrluck":     {},
		"jrrp":       {},
		"kk":         {},
		"ktv":        {},
		"luck":       {},
		"pic":        {},
		"randpic":    {},
		"save":       {},
		"saying":     {},
		"unban":      {},
		"吃什么":        {},
		"帮助":         {},
		"帮助菜单":       {},
		"名言":         {},
		"点歌":         {},
		"猜歌":         {},
		"猜歌提示":       {},
		"猜歌放弃":       {},
		"保存":         {},
		"保存图片":       {},
		"保存表情":       {},
		"语录":         {},
		"随机图片":       {},
		"啃":          {},
	}
	knownBareCommands = map[string]struct{}{
		"help": {},
		"帮助":   {},
		"帮助菜单": {},
	}
	legacyRandPicAliases = map[string]string{
		"fu":     "fu",
		"fufu":   "fu",
		"gst":    "gst",
		"gu":     "gu",
		"kira":   "kira",
		"motohg": "motohg",
		"pjsk":   "pjsk",
		"rui":    "rui",
		"wt":     "wt",
		"tls":    "tls",
	}
)

func init() {
	zero.OnMessage().
		SetPriority(-1000).
		SetBlock(false).
		Handle(handleCommandDebugLog)
}

func handleCommandDebugLog(ctx *zero.Ctx) {
	command, args, ok := detectCommandInvocation(ctx)
	if !ok {
		return
	}

	scope := "private"
	if ctx != nil && ctx.Event != nil && ctx.Event.GroupID != 0 {
		scope = "group"
	}
	log.Debugf(
		"[cmd] invoked scope=%s group=%d user=%d command=%q args=%q",
		scope,
		ctx.Event.GroupID,
		ctx.Event.UserID,
		command,
		preview(args),
	)
}

func detectCommandInvocation(ctx *zero.Ctx) (string, string, bool) {
	if ctx == nil || ctx.Event == nil {
		return "", "", false
	}

	if match, ok := commandutil.MatchReplyableCommand(ctx.Event.Message, knownPrefixedCommandList()...); ok {
		return match.Command, match.Args, true
	}

	plain := strings.TrimSpace(ctx.ExtractPlainText())
	if _, ok := knownBareCommands[plain]; ok {
		return plain, "", true
	}

	return "", "", false
}

func knownPrefixedCommandList() []string {
	commands := make([]string, 0, len(knownPrefixedCommands))
	for command := range knownPrefixedCommands {
		commands = append(commands, command)
	}
	for command := range directRandPicCommands() {
		commands = append(commands, command)
	}
	return commands
}

func directRandPicCommands() map[string]struct{} {
	commands := make(map[string]struct{})

	for command := range legacyRandPicAliases {
		commands[command] = struct{}{}
	}

	if categories, err := database.DistinctRandPicCategories(context.Background(), nil); err == nil && len(categories) > 0 {
		for _, category := range categories {
			category = strings.ToLower(strings.TrimSpace(category))
			if category != "" {
				commands[category] = struct{}{}
			}
		}
		return commands
	}

	if cfg := config.Get(); cfg != nil {
		for _, category := range cfg.RandPic.CommandList {
			category = strings.ToLower(strings.TrimSpace(category))
			if category != "" {
				commands[category] = struct{}{}
			}
		}
	}

	baseDir := "data/randpic"
	if cfg := config.Get(); cfg != nil && strings.TrimSpace(cfg.RandPic.StoreDirPath) != "" {
		baseDir = strings.TrimSpace(cfg.RandPic.StoreDirPath)
	}
	if entries, err := os.ReadDir(baseDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(entry.Name()))
			if name != "" {
				commands[name] = struct{}{}
			}
		}
	}

	return commands
}

func preview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= 120 {
		return text
	}
	return string(runes[:120]) + "..."
}
