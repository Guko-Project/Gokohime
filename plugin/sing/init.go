// Package sing provides asynchronous DiffSinger chorus covers.
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
	for _, command := range []string{"sing", "msing", "ssing"} {
		zero.OnCommand(command, commandBoundary).SetPriority(30).SetBlock(true).Handle(handleSing)
	}
}

func commandBoundary(ctx *zero.Ctx) bool {
	command, _ := ctx.State["command"].(string)
	if command != "sing" && command != "msing" && command != "ssing" {
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
	if command == "msing" {
		mode = "mimic"
	}
	query, _ := ctx.State["args"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		ctx.SendChain(message.Text("用法：.sing [歌名]（默认 score）\n.msing [歌名]（mimic）\n.ssing [歌名]（score）\n例如：.sing 千本樱\n会翻唱歌曲的一段副歌。"))
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
		runSing(target, query, mode, settings)
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

func runSing(target destination, query, mode string, cfg config.SingConfig) {
	timeout := time.Duration(cfg.PollTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := newClient(cfg.BaseURL, cfg.APIKey, mode)
	fail := func(err error) {
		log.Warnf("[sing] request failed: %v", err)
		reply := "这首暂时没能唱出来，请稍后再试试！"
		switch {
		case errors.Is(err, errNoSong):
			reply = "没有找到这首歌，试试更完整的歌名，或者加上歌手名吧！"
		case errors.Is(err, context.DeadlineExceeded):
			reply = "这首准备得太久了，等待超时，请稍后再试试！"
		}
		target.send(message.Text(reply))
	}
	selected, err := c.search(ctx, query)
	if err != nil {
		fail(err)
		return
	}
	taskID, err := c.submit(ctx, selected.ID)
	if err != nil {
		fail(err)
		return
	}
	log.Infof("[sing] submitted task=%s song_id=%s mode=%s", taskID, selected.ID, mode)
	if err := c.wait(ctx, taskID, time.Duration(cfg.PollIntervalSec)*time.Second); err != nil {
		fail(fmt.Errorf("task %s: %w", taskID, err))
		return
	}
	audio, err := c.audio(ctx, taskID)
	if err != nil {
		fail(fmt.Errorf("task %s: %w", taskID, err))
		return
	}
	if !target.send(message.Record("base64://" + base64.StdEncoding.EncodeToString(audio))) {
		log.Warnf("[sing] voice delivery failed: task=%s", taskID)
		target.send(message.Text("已经唱好了，但语音发送失败了，请稍后再试！"))
	}
}
