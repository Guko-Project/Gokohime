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
.kks / .kksadd / .kksdel - KK 歌单随机歌
.saying - 查看已迁移到 randpic 的语录分类
.save (回复表情) - 保存表情
.cg cstart / .猜歌 - 中V猜歌
.cg jstart - 日V猜歌
.score - 自填简谱和歌词（发送命令查看示例）
.song - 唱歌命令帮助（带参数同 .sing）
.sing / .ssing [音源] 歌名 [原唱] 或歌词 - score 翻唱（默认洛天依）
.msing [音源] 歌名 [原唱] 或歌词 - mimic 翻唱（默认洛天依）
.bsong / .bmsong [音源] 歌名或歌词 - 带原曲伴奏翻唱
点歌命令的 sing / song 拼法互通（如 .bsing = .bsong、.bmsing = .bmsong）
.voices - 查看音源和别名（洛 / 初 / 初日 / 心）；末尾可加 0-120 指定秒数范围

@我 或直接聊天 - AI 对话`

func init() {
	zero.OnFullMatchGroup([]string{".help", ".帮助", ".帮助菜单", "help", "帮助", "帮助菜单"}).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			if _, err := os.Stat(helpImagePath); err == nil {
				if abs, err := filepath.Abs(helpImagePath); err == nil {
					if img, err := imageutil.LocalImageSegment(abs); err == nil {
						ctx.SendChain(img)
						ctx.SendChain(message.Text(".score - 自填简谱和歌词（发送命令查看示例）\n.song - 唱歌命令帮助（带参数同 .sing）\n.sing / .ssing [音源] 歌名 [原唱] 或歌词 - score 翻唱（默认洛天依）\n.msing [音源] 歌名 [原唱] 或歌词 - mimic 翻唱（默认洛天依）\n.bsong / .bmsong [音源] 歌名或歌词 - 带原曲伴奏翻唱\n点歌命令的 sing / song 拼法互通（如 .bsing = .bsong、.bmsing = .bmsong）\n.voices - 查看音源和别名（洛 / 初 / 初日 / 心）；末尾可加 0-120 指定秒数范围"))
						return
					}
				}
			}
			ctx.SendChain(message.Text(helpText))
		})
}
