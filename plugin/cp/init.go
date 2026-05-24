package cp

import (
	"context"
	"errors"
	"strings"

	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const usageText = "使用 .cp A B 来生成CP短打！"
const emptyStoryText = "当前还没有 CP 文案可用。"

func init() {
	zero.OnCommand("cp").SetBlock(true).Handle(handleCP)
}

func handleCP(ctx *zero.Ctx) {
	args, _ := ctx.State["args"].(string)
	if strings.TrimSpace(args) == "" {
		ctx.SendChain(message.Text(usageText))
		return
	}

	names, ok := parseCPArgs(args)
	if !ok {
		ctx.SendChain(message.Text("要输入两个人的名字才行哦~"))
		return
	}

	record, err := database.RandomCPStory(context.Background(), nil)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.SendChain(message.Text(emptyStoryText))
			return
		}
		ctx.SendChain(message.Text("读取 CP 数据失败。"))
		return
	}

	story := renderCPStory(record.Story, names[0], names[1])
	ctx.SendChain(message.Text(story))
}

func parseCPArgs(args string) ([2]string, bool) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) != 2 {
		return [2]string{}, false
	}
	return [2]string{fields[0], fields[1]}, true
}

func renderCPStory(template, gong, shou string) string {
	story := strings.ReplaceAll(template, "<攻>", gong)
	return strings.ReplaceAll(story, "<受>", shou)
}
