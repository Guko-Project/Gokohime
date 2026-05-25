package stickersaver

import (
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func init() {
	zero.OnCommandGroup([]string{"save", "保存图片", "保存表情", "保存"}).
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
