package saying

import (
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var legacyCategories = []string{"fu", "gst", "gu", "kira", "motohg", "pjsk", "rui", "tls", "wt"}

func init() {
	// Compatibility entry only. Direct category commands are now handled by plugin/randpic.
	zero.OnCommandGroup([]string{"saying", "语录", "名言"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			args, _ := ctx.State["args"].(string)
			if strings.TrimSpace(args) != "" {
				ctx.SendChain(message.Text("语录分类命令已迁移到 randpic，请直接使用 .<分类>，例如 .gst 或 .fu 关键词"))
				return
			}
			ctx.SendChain(message.Text("语录分类已迁移到 randpic，可用分类: " + strings.Join(legacyCategories, ", ")))
		})
}
