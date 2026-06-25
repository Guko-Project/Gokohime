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
.kadd 歌名 分类 [BV/链接] - 添加歌曲
.kdel 歌名 - 删除歌曲

例：
.kadd 群青 日 BV1xx411c7mD
.kadd Little Wish 粥批
.kadd Little Wish | 粥批 | https://www.bilibili.com/video/BV1xx411c7mD

提示：歌名含空格时，默认最后一段是分类；需要避免歧义可用 | 分隔。
.kdel 群青`

func init() {
	zero.OnCommandGroup([]string{"kk", "ktv", "点歌"}, exactKTVCommandRule("kk", "ktv", "点歌")).
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

	zero.OnCommandGroup([]string{"kadd"}, exactKTVCommandRule("kadd")).
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
			song.Issuer = fmt.Sprint(ctx.Event.UserID)
			song.EntryHash = hashKTVSong(song.Name)
			if err := database.UpsertKTVSong(context.Background(), nil, song); err != nil {
				ctx.SendChain(message.Text("添加歌曲失败了，请稍后再试~"))
				return
			}

			if exists {
				ctx.SendChain(message.Text(fmt.Sprintf("《%s》已更新 KTV 曲库信息！", song.Name)))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("《%s》已加入 KTV 曲库！", song.Name)))
		})

	zero.OnCommandGroup([]string{"kdel"}, exactKTVCommandRule("kdel")).
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

func exactKTVCommandRule(commands ...string) zero.Rule {
	return func(ctx *zero.Ctx) bool {
		if len(ctx.Event.Message) == 0 || ctx.Event.Message[0].Type != "text" {
			return false
		}
		return isExactKTVCommandText(ctx.Event.Message[0].Data["text"], zero.BotConfig.CommandPrefix, commands...)
	}
}

func isExactKTVCommandText(text, prefix string, commands ...string) bool {
	if !strings.HasPrefix(text, prefix) {
		return false
	}
	cmdMessage := strings.TrimPrefix(text, prefix)
	for _, command := range commands {
		if cmdMessage == command || strings.HasPrefix(cmdMessage, command+" ") {
			return true
		}
	}
	return false
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
		if len(fields) == 1 {
			name = fields[0]
		} else if len(fields) > 1 {
			last := fields[len(fields)-1]
			if isKTVVideoRef(last) {
				bv = last
				fields = fields[:len(fields)-1]
			}

			if len(fields) == 1 {
				name = fields[0]
			} else if len(fields) > 1 {
				category = fields[len(fields)-1]
				name = strings.Join(fields[:len(fields)-1], " ")
			}
		}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ktv song name is empty")
	}
	return &database.KTVSong{Name: name, Category: category, BV: bv}, nil
}

func isKTVVideoRef(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	return strings.HasPrefix(value, "BV") ||
		strings.Contains(lower, "bilibili.com/video/") ||
		strings.Contains(lower, "b23.tv/")
}

func extractArgs(ctx *zero.Ctx) string {
	args, _ := ctx.State["args"].(string)
	return strings.TrimSpace(args)
}

func hashKTVSong(name string) string {
	sum := sha256.Sum256([]byte("ktv\n" + strings.TrimSpace(name)))
	return hex.EncodeToString(sum[:])
}
