package kk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

func init() {
	zero.OnCommandGroup([]string{"kk", "ktv", "点歌"}).
		SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args, _ := ctx.State["args"].(string)
		category := strings.TrimSpace(args)
		song, err := database.RandomKTVSong(context.Background(), nil, category)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				ctx.SendChain(message.Text("亲亲没有这种类型的歌曲哦！"))
				return
			}
			ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
			return
		}

		if song.BV != "" {
			ctx.SendChain(message.Text(fmt.Sprintf("来唱《%s》!\n播放地址：%s", song.Name, song.BV)))
			return
		}
		ctx.SendChain(message.Text(fmt.Sprintf("来唱《%s》!", song.Name)))
	})
}
