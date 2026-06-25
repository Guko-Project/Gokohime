package kk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const usageText = `【KTV 曲库】
.ktv [分类] - 随机推荐歌曲
.kadd 歌名 [分类] [BV/链接] - 添加歌曲
.kdel 歌名 - 删除歌曲

例：
.kadd 群青 日 BV1xx411c7mD
.kadd 群青 | 日 | https://www.bilibili.com/video/BV1xx411c7mD
.kdel 群青`

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

	zero.OnCommandGroup([]string{"kadd"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			song, err := parseKTVAddArgs(extractArgs(ctx))
			if err != nil {
				ctx.SendChain(message.Text(usageText))
				return
			}

			exists, err := database.KTVSongExists(context.Background(), nil, song.Name)
			if err != nil {
				ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
				return
			}
			if exists {
				ctx.SendChain(message.Text(fmt.Sprintf("《%s》已经在 KTV 曲库里了哦！", song.Name)))
				return
			}

			song.Issuer = fmt.Sprint(ctx.Event.UserID)
			song.EntryHash = hashKTVSong(song.Name)
			if err := database.AddKTVSong(context.Background(), nil, song); err != nil {
				ctx.SendChain(message.Text("添加歌曲失败了，请稍后再试~"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("《%s》已加入 KTV 曲库！", song.Name)))
		})

	zero.OnCommandGroup([]string{"kdel"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			name := strings.TrimSpace(extractArgs(ctx))
			if name == "" {
				ctx.SendChain(message.Text(usageText))
				return
			}

			exists, err := database.KTVSongExists(context.Background(), nil, name)
			if err != nil {
				ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
				return
			}
			if !exists {
				ctx.SendChain(message.Text(fmt.Sprintf("《%s》还不在 KTV 曲库里哦。", name)))
				return
			}

			if err := database.DeleteKTVSong(context.Background(), nil, name); err != nil {
				ctx.SendChain(message.Text("删除歌曲失败了，请稍后再试~"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("《%s》已从 KTV 曲库删除。", name)))
		})
}

func parseKTVAddArgs(args string) (*database.KTVSong, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil, errors.New("empty ktv add args")
	}

	var name, category, bv string
	if strings.Contains(args, "|") {
		parts := strings.Split(args, "|")
		if len(parts) > 0 {
			name = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 {
			category = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			bv = strings.TrimSpace(strings.Join(parts[2:], "|"))
		}
	} else {
		fields := strings.Fields(args)
		if len(fields) > 0 {
			name = fields[0]
		}
		if len(fields) > 1 {
			category = fields[1]
		}
		if len(fields) > 2 {
			bv = strings.Join(fields[2:], " ")
		}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ktv song name is empty")
	}
	return &database.KTVSong{Name: name, Category: category, BV: bv}, nil
}

func extractArgs(ctx *zero.Ctx) string {
	args, _ := ctx.State["args"].(string)
	return strings.TrimSpace(args)
}

func hashKTVSong(name string) string {
	sum := sha256.Sum256([]byte("ktv\n" + strings.TrimSpace(name)))
	return hex.EncodeToString(sum[:])
}
