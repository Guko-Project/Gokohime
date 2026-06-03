package commandutil

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type Match struct {
	Prefix  string
	Command string
	Args    string
}

func ReplyableCommandRule(commands ...string) zero.Rule {
	normalizedCommands := normalizeCommands(commands)

	return func(ctx *zero.Ctx) bool {
		if ctx == nil || ctx.Event == nil {
			return false
		}

		match, ok := matchReplyableCommand(ctx.Event.Message, normalizedCommands)
		if !ok {
			return false
		}

		if ctx.State == nil {
			ctx.State = zero.State{}
		}
		ctx.State["prefix"] = match.Prefix
		ctx.State["command"] = match.Command
		ctx.State["args"] = match.Args
		return true
	}
}

func MatchReplyableCommand(msg message.Message, commands ...string) (Match, bool) {
	return matchReplyableCommand(msg, normalizeCommands(commands))
}

func matchReplyableCommand(msg message.Message, commands []string) (Match, bool) {
	textIndex := firstCommandTextSegment(msg)
	if textIndex < 0 {
		return Match{}, false
	}

	firstText := msg[textIndex].Data["text"]
	for _, prefix := range AllowedCommandPrefixes() {
		if !strings.HasPrefix(firstText, prefix) {
			continue
		}

		cmdMessage := firstText[len(prefix):]
		if startsWithWhitespace(cmdMessage) {
			continue
		}

		for _, command := range commands {
			if !strings.HasPrefix(cmdMessage, command) {
				continue
			}

			args := strings.TrimLeft(cmdMessage[len(command):], " ")
			if len(msg) > textIndex+1 {
				args += msg[textIndex+1:].ExtractPlainText()
			}

			return Match{
				Prefix:  prefix,
				Command: command,
				Args:    args,
			}, true
		}
	}

	return Match{}, false
}

func AllowedCommandPrefixes() []string {
	prefixes := make([]string, 0, 2)

	prefix := "."
	if cfg := config.Get(); cfg != nil {
		if value := strings.TrimSpace(cfg.Bot.CommandPrefix); value != "" {
			prefix = value
		}
	}

	prefixes = appendUnique(prefixes, prefix)
	prefixes = appendUnique(prefixes, "/")
	return prefixes
}

func normalizeCommands(commands []string) []string {
	normalized := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.ToLower(strings.TrimSpace(command))
		if command == "" {
			continue
		}
		normalized = append(normalized, command)
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		if len(normalized[i]) == len(normalized[j]) {
			return normalized[i] < normalized[j]
		}
		return len(normalized[i]) > len(normalized[j])
	})

	return normalized
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstCommandTextSegment(msg message.Message) int {
	for i, seg := range msg {
		if seg.Type == "text" {
			return i
		}
	}
	return -1
}

func startsWithWhitespace(input string) bool {
	if input == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(input)
	return unicode.IsSpace(r)
}
