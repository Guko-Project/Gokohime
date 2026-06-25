package kks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const usageText = `【KK 歌单】
.kks - 随机推荐歌曲
.kks 分类 - 随机推荐指定分类歌曲
.kks list - 查看自己创建的歌单，最多 10 个
.kksadd 分类 链接 - 导入/覆盖分类歌单
.kksdel 分类 - 删除整个分类
.kksdel 分类 歌曲 - 删除分类下的某首歌曲`

const songlistTimeout = 3 * time.Minute

var (
	songlistEndpoint   = "https://sss.unmeta.cn/songlist?detailed=false&format=song&order=normal"
	songlistHTTPClient = &http.Client{Timeout: songlistTimeout}
)

type songlistResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Name       string   `json:"name"`
		Songs      []string `json:"songs"`
		SongsCount int      `json:"songs_count"`
	} `json:"data"`
}

func init() {
	zero.OnCommandGroup([]string{"kks"}, exactKKSCommandRule("kks")).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			args := extractArgs(ctx)
			if strings.EqualFold(args, "list") {
				handleKKSList(ctx)
				return
			}
			handleKKSRandom(ctx, args)
		})

	zero.OnCommandGroup([]string{"kksadd"}, exactKKSCommandRule("kksadd")).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			category, playlistURL, err := parseKKSAddArgs(extractArgs(ctx))
			if err != nil {
				ctx.SendChain(message.Text(usageText))
				return
			}

			issuer := fmt.Sprint(ctx.Event.UserID)
			owner, exists, err := database.KKSonglistCategoryOwner(context.Background(), nil, category)
			if err != nil {
				ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
				return
			}
			if exists && owner != issuer && !isKKSAdmin(ctx.Event.UserID) {
				ctx.SendChain(message.Text("这个分类不是你创建的，不能覆盖哦。"))
				return
			}

			songs, err := fetchSonglist(context.Background(), playlistURL)
			if err != nil {
				ctx.SendChain(message.Text(fmt.Sprintf("歌单拉取失败：%s", err.Error())))
				return
			}
			if err := database.ReplaceKKSonglistCategory(context.Background(), nil, category, issuer, playlistURL, songs); err != nil {
				ctx.SendChain(message.Text("歌单写入失败了，请稍后再试~"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("分类「%s」已导入 %d 首歌曲。", category, len(nonEmptySongs(songs)))))
		})

	zero.OnCommandGroup([]string{"kksdel"}, exactKKSCommandRule("kksdel")).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			category, songName, err := parseKKSDelArgs(extractArgs(ctx))
			if err != nil {
				ctx.SendChain(message.Text(usageText))
				return
			}

			owner, exists, err := database.KKSonglistCategoryOwner(context.Background(), nil, category)
			if err != nil {
				ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
				return
			}
			if !exists {
				ctx.SendChain(message.Text(fmt.Sprintf("分类「%s」还不存在哦。", category)))
				return
			}
			if owner != fmt.Sprint(ctx.Event.UserID) && !isKKSAdmin(ctx.Event.UserID) {
				ctx.SendChain(message.Text("这个分类不是你创建的，不能删除哦。"))
				return
			}

			if songName == "" {
				deleted, err := database.DeleteKKSonglistCategory(context.Background(), nil, category)
				if err != nil {
					ctx.SendChain(message.Text("删除分类失败了，请稍后再试~"))
					return
				}
				ctx.SendChain(message.Text(fmt.Sprintf("分类「%s」已删除，共删除 %d 首歌曲。", category, deleted)))
				return
			}

			deleted, err := database.DeleteKKSonglistSong(context.Background(), nil, category, songName)
			if err != nil {
				ctx.SendChain(message.Text("删除歌曲失败了，请稍后再试~"))
				return
			}
			if deleted == 0 {
				ctx.SendChain(message.Text(fmt.Sprintf("分类「%s」里没有《%s》哦。", category, songName)))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("已从「%s」删除 %d 首《%s》。", category, deleted, songName)))
		})
}

func exactKKSCommandRule(commands ...string) zero.Rule {
	return func(ctx *zero.Ctx) bool {
		if len(ctx.Event.Message) == 0 || ctx.Event.Message[0].Type != "text" {
			return false
		}
		return isExactKKSCommandText(ctx.Event.Message[0].Data["text"], zero.BotConfig.CommandPrefix, commands...)
	}
}

func isExactKKSCommandText(text, prefix string, commands ...string) bool {
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

func handleKKSRandom(ctx *zero.Ctx, category string) {
	category = strings.TrimSpace(category)
	song, err := database.RandomKKSonglistSong(context.Background(), nil, category)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if category == "" {
				ctx.SendChain(message.Text("亲亲还没有可随机的歌单哦！"))
				return
			}
			ctx.SendChain(message.Text("亲亲没有这个分类的歌单哦！"))
			return
		}
		ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
		return
	}

	if category == "" {
		ctx.SendChain(message.Text(fmt.Sprintf("来听《%s》！\n分类：%s", song.Name, song.Category)))
		return
	}
	ctx.SendChain(message.Text(fmt.Sprintf("来听《%s》！", song.Name)))
}

func handleKKSList(ctx *zero.Ctx) {
	issuer := fmt.Sprint(ctx.Event.UserID)
	summaries, err := database.ListKKSonglistCategoriesByIssuer(context.Background(), nil, issuer, 10)
	if err != nil {
		ctx.SendChain(message.Text("歌单读取失败了，请稍后再试~"))
		return
	}
	if len(summaries) == 0 {
		ctx.SendChain(message.Text("你还没有创建过歌单哦。"))
		return
	}

	var b strings.Builder
	b.WriteString("你创建的歌单：")
	for i, summary := range summaries {
		b.WriteString(fmt.Sprintf("\n%d. %s（%d 首）", i+1, summary.Category, summary.Count))
	}
	ctx.SendChain(message.Text(b.String()))
}

func parseKKSAddArgs(args string) (string, string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) < 2 {
		return "", "", errors.New("invalid kksadd args")
	}
	category := strings.TrimSpace(fields[0])
	playlistURL := strings.TrimSpace(strings.Join(fields[1:], " "))
	if category == "" || strings.EqualFold(category, "list") || playlistURL == "" {
		return "", "", errors.New("invalid kksadd args")
	}
	return category, playlistURL, nil
}

func parseKKSDelArgs(args string) (string, string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return "", "", errors.New("invalid kksdel args")
	}
	category := strings.TrimSpace(fields[0])
	if category == "" || strings.EqualFold(category, "list") {
		return "", "", errors.New("invalid kksdel args")
	}
	if len(fields) == 1 {
		return category, "", nil
	}
	return category, strings.TrimSpace(strings.Join(fields[1:], " ")), nil
}

func fetchSonglist(ctx context.Context, playlistURL string) ([]string, error) {
	playlistURL = strings.TrimSpace(playlistURL)
	if playlistURL == "" {
		return nil, errors.New("链接不能为空")
	}

	form := url.Values{}
	form.Set("url", playlistURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, songlistEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://music.unmeta.cn")
	req.Header.Set("Referer", "https://music.unmeta.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GokohimeBot/1.0)")

	resp, err := songlistHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("接口返回 HTTP %d", resp.StatusCode)
	}

	var payload songlistResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Code != 1 {
		if strings.TrimSpace(payload.Msg) != "" {
			return nil, errors.New(payload.Msg)
		}
		return nil, errors.New("接口返回失败")
	}

	songs := nonEmptySongs(payload.Data.Songs)
	if len(songs) == 0 {
		return nil, errors.New("歌单为空")
	}
	return songs, nil
}

func nonEmptySongs(songs []string) []string {
	result := make([]string, 0, len(songs))
	for _, song := range songs {
		if name := strings.TrimSpace(song); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func isKKSAdmin(userID int64) bool {
	cfg := config.Get()
	if cfg == nil {
		return false
	}
	for _, superUser := range cfg.Bot.SuperUsers {
		if superUser == userID {
			return true
		}
	}
	return false
}

func extractArgs(ctx *zero.Ctx) string {
	args, _ := ctx.State["args"].(string)
	return strings.TrimSpace(args)
}
