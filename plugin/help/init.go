package help

import (
	"os"
	"path/filepath"

	"github.com/colanns/gokohime/internal/imageutil"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const helpImagePath = "data/help.png"

const helpText = `【鸽子姬 帮助菜单】
.help - 显示此帮助
.eat - 正经饭饭推荐
.eats - 校内食堂推荐
.eatwhat - 不正经的饭饭
.eadd / .edel - 管理豪华菜单
.jrluck / .luck - 今日运势
.chp A B - CP名生成
.cp A B - CP短打
.kk / .ktv - KTV随机歌
.kadd / .kdel - 管理 KTV 曲库
.saying - 查看已迁移到 randpic 的语录分类
.save (回复表情) - 保存表情
.cg jstart - 猜歌游戏

@我 或直接聊天 - AI 对话`

func init() {
	zero.OnFullMatchGroup([]string{".help", ".帮助", ".帮助菜单", "help", "帮助", "帮助菜单"}).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			if _, err := os.Stat(helpImagePath); err == nil {
				if abs, err := filepath.Abs(helpImagePath); err == nil {
					if img, err := imageutil.LocalImageSegment(abs); err == nil {
						ctx.SendChain(img)
						return
					}
				}
			}
			ctx.SendChain(message.Text(helpText))
		})
}
