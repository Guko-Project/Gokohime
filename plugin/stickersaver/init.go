package stickersaver

import (
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var commands = []string{"save", "保存图片", "保存表情", "保存"}

func init() {
	zero.On("message", replyCommandRule(commands...)).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			replyMsg := extractReplyMessage(ctx)
			if replyMsg == nil {
				ctx.SendChain(message.Text("只有回复表情才可以用捏"))
				return
			}

			for _, seg := range replyMsg {
				if seg.Type != "image" {
					continue
				}
				url := seg.Data["url"]
				if url == "" {
					continue
				}
				ctx.SendChain(message.Text("表情："), message.Image(url), message.Text("\n原始链接："+url))
				return
			}

			ctx.SendChain(message.Text("未在回复内容中检测到表情..."))
		})
}

// replyCommandRule matches messages that contain a reply segment followed by a command.
func replyCommandRule(cmds ...string) zero.Rule {
	return func(ctx *zero.Ctx) bool {
		msg := ctx.Event.Message
		if len(msg) == 0 {
			return false
		}

		// Find the first text segment (may be after a reply segment)
		var text string
		for _, seg := range msg {
			if seg.Type == "text" {
				text = strings.TrimSpace(seg.Data["text"])
				break
			}
		}
		if text == "" {
			return false
		}

		prefix := zero.BotConfig.CommandPrefix
		if !strings.HasPrefix(text, prefix) {
			return false
		}
		cmdText := text[len(prefix):]

		for _, cmd := range cmds {
			if strings.HasPrefix(cmdText, cmd) {
				ctx.State["command"] = cmd
				ctx.State["args"] = strings.TrimSpace(cmdText[len(cmd):])
				return true
			}
		}
		return false
	}
}

func extractReplyMessage(ctx *zero.Ctx) message.Message {
	if ctx == nil || ctx.Event == nil {
		return nil
	}
	for _, seg := range ctx.Event.Message {
		if seg.Type != "reply" || seg.Data["id"] == "" {
			continue
		}
		msg := ctx.GetMessage(seg.Data["id"])
		return msg.Elements
	}
	return nil
}
