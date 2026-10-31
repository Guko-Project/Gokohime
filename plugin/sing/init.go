// Package sing provides asynchronous DiffSinger covers selected by title or lyric.
package sing

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var warmups = []string{
	"让我润润喉咙！",
	"这个我会，让我酝酿一下——",
	"这个，这个怎么唱来着... 我想想！",
}
var pending sync.Map

type destination struct{ selfID, groupID, userID int64 }

func init() {
	zero.OnCommand("score", commandBoundary).SetPriority(30).SetBlock(true).Handle(handleScore)
	for _, command := range []string{"sing", "msing", "ssing", "song", "bsong", "bmsong"} {
		zero.OnCommand(command, commandBoundary).SetPriority(30).SetBlock(true).Handle(handleSing)
	}
	zero.OnCommand("voices", commandBoundary).SetPriority(30).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		cfg := config.Get()
		if cfg == nil || cfg.Sing.APIKey == "" {
			ctx.SendChain(message.Text("唱歌功能还没有配置好，请联系管理员。"))
			return
		}
		target := destination{ctx.Event.SelfID, ctx.Event.GroupID, ctx.Event.UserID}
		settings := cfg.Sing
		go func() {
			request, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			catalog, err := newClient(settings.BaseURL, settings.APIKey, "score").voices(request)
			if err != nil {
				target.send(message.Text("音源列表暂时无法读取，请稍后再试。"))
				return
			}
			lines := []string{"音源列表（默认洛天依）："}
			for _, v := range catalog.Voices {
				status := ""
				if !v.Available {
					status = "（暂不可用）"
				}
				lines = append(lines, v.Name+status+"："+strings.Join(v.Aliases, " / "))
			}
			lines = append(lines, "用法：.sing 洛 歌名或歌词；三句及以上歌词完整匹配后按指定范围演唱。")
			target.send(message.Text(strings.Join(lines, "\n")))
		}()
	})
}

func commandBoundary(ctx *zero.Ctx) bool {
	command, _ := ctx.State["command"].(string)
	if command != "sing" && command != "msing" && command != "ssing" && command != "voices" && command != "song" && command != "bsong" && command != "bmsong" && command != "score" {
		return false
	}
	text := ctx.Event.Message[0].Data["text"]
	rest, ok := strings.CutPrefix(text, zero.BotConfig.CommandPrefix+command)
	if !ok || rest == "" {
		return ok
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsSpace(r)
}

func handleSing(ctx *zero.Ctx) {
	command, _ := ctx.State["command"].(string)
	mode := "score"
	if command == "msing" || command == "bmsong" {
		mode = "mimic"
	}
	query, _ := ctx.State["args"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		ctx.SendChain(message.ImageBytes(singHelpImage))
		return
	}
	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.Sing.APIKey) == "" {
		ctx.SendChain(message.Text("唱歌功能还没有配置好，请联系管理员。"))
		return
	}
	target := destination{ctx.Event.SelfID, ctx.Event.GroupID, ctx.Event.UserID}
	if _, loaded := pending.LoadOrStore(target, struct{}{}); loaded {
		ctx.SendChain(message.Text("上一首还在准备中，等我唱完再点吧！"))
		return
	}
	ctx.SendChain(message.Text(warmups[rand.IntN(len(warmups))]))
	// Snapshot only scalar IDs/config: ZeroBot reuses its event and matcher context.
	settings := cfg.Sing
	go func() {
		defer pending.Delete(target)
		runSing(target, query, mode, command == "bsong" || command == "bmsong", settings)
	}()
}

func (d destination) send(segments ...message.Segment) bool {
	bot := zero.GetBot(d.selfID)
	if bot == nil {
		return false
	}
	msg := message.Message(segments)
	if d.groupID != 0 {
		return bot.SendGroupMessage(d.groupID, msg) != 0
	}
	return bot.SendPrivateMessage(d.userID, msg) != 0
}

func runSing(target destination, query, mode string, withAccompaniment bool, cfg config.SingConfig) {
	timeout := time.Duration(cfg.PollTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := newClient(cfg.BaseURL, cfg.APIKey, mode)
	c.withAccompaniment = withAccompaniment
	songName := query
	fail := func(err error) {
		log.Warnf("[sing] request failed: %v", err)
		reply := "这首暂时没能唱出来，请稍后再试试！"
		switch {
		case errors.Is(err, errTimeRange):
			reply = "时间范围格式为 起始秒-结束秒，须满足 0 ≤ 起始 < 结束，例如：.sing 初 歌名 0-120"
		case errors.Is(err, errEmptyQuery):
			reply = "请在音源名后填写歌名或歌词，例如：.sing 洛 雨爱 杨丞琳"
		case errors.Is(err, errVoiceUnavailable), errors.Is(err, httpError(503)):
			reply = "所选音源暂不可用，请用 .voices 查看音源列表。"
		case errors.Is(err, errLyricMatch):
			reply = "完整歌词未匹配，请检查歌词内容与歌曲版本；不会改唱随机片段。"
		case errors.Is(err, errNoSong):
			reply = "没有找到这首歌，试试歌名加歌手，或者更完整的一句歌词吧！"
		case errors.Is(err, context.DeadlineExceeded):
			reply = "这首准备得太久了，等待超时，请稍后再试试！"
		}
		if detail, ok := generationReply(err, songName); ok {
			reply = detail
		}
		target.send(message.Text(reply))
	}
	var err error
	query, c.start, c.end, err = parseTimeRange(query)
	if err != nil {
		fail(err)
		return
	}
	catalog, err := c.voices(ctx)
	if err != nil {
		fail(err)
		return
	}
	chosen, query, err := selectVoice(query, catalog)
	if err != nil {
		fail(err)
		return
	}
	c.voiceID = chosen.ID
	songName = query
	selected, err := c.resolve(ctx, query)
	if err != nil {
		fail(err)
		return
	}
	if selected.Song.Name != "" {
		songName = selected.Song.Name
	} else {
		songName = "歌曲 " + selected.Song.ID
	}
	// Long explicit excerpts receive a duration-scaled budget, including the
	// generation and delivery. The configured short-job timeout remains a floor.
	span := selected.Match.End - selected.Match.Start
	if c.start != nil {
		span = *c.end - *c.start
	}
	if span > 32 {
		budget := timeout + time.Duration(span*60)*time.Second
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), budget)
		defer cancel()
	}
	taskID, err := c.submit(ctx, selected.Song.ID, selected.LyricQuery)
	if err != nil {
		fail(err)
		return
	}
	log.Infof("[sing] submitted task=%s song_id=%s mode=%s match=%s", taskID, selected.Song.ID, mode, selected.MatchType)
	if err := deliverSong(ctx, target, c, taskID, time.Duration(cfg.PollIntervalSec)*time.Second); err != nil {
		fail(err)
	}
}

func deliverSong(ctx context.Context, target destination, c *client, taskID string, interval time.Duration) error {
	if err := c.wait(ctx, taskID, interval); err != nil {
		return fmt.Errorf("task %s: %w", taskID, err)
	}
	kinds := c.parts
	if len(kinds) == 0 {
		kinds = []string{c.outputKind()}
	}
	for _, kind := range kinds {
		audio, err := c.audioKind(ctx, taskID, kind)
		if err != nil {
			return fmt.Errorf("task %s: %w", taskID, err)
		}
		if !target.send(message.Record("base64://" + base64.StdEncoding.EncodeToString(audio))) {
			log.Warnf("[sing] voice delivery failed: task=%s", taskID)
			target.send(message.Text("已经唱好了，但语音发送失败了，请稍后再试！"))
			return nil
		}
	}
	return nil
}
