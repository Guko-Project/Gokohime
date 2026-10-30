package guesssong

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"

	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const (
	usageText           = "猜歌说明：\n使用 .cg jstart 开始日V猜歌\n使用 .cg cstart 或 .猜歌 开始中V猜歌\n使用 .cg hint 获取帮助，第一次是另一段歌曲，第二次是P主/演唱者名称\n使用 .cg gg 放弃猜歌\n使用 .cg 歌名 做出回答\n使用 .cg help 获取本帮助信息"
	sliceLengthSec      = 6
	downloadFailureText = "下载歌曲失败惹！可以联系 Colanns 看看怎么回事——"
)

var songs = newSongService("data/guesssong", newNeteaseClient())

// Retain only running commands; every command for a session shares this guard.
var busySessions sync.Map

func serialized(handler func(*zero.Ctx)) func(*zero.Ctx) {
	return func(ctx *zero.Ctx) {
		id := guessSessionID(ctx)
		if _, busy := busySessions.LoadOrStore(id, struct{}{}); busy {
			ctx.SendChain(message.Text("正在处理本场猜歌，请稍候再试。"))
			return
		}
		defer busySessions.Delete(id)
		handler(ctx)
	}
}

func init() {
	zero.OnCommand("cg").SetBlock(true).Handle(serialized(handleGuessSongCommand))
	zero.OnCommand("猜歌").SetBlock(true).Handle(serialized(func(ctx *zero.Ctx) {
		startGame(ctx, "cv")
	}))
	zero.OnCommand("猜歌提示").SetBlock(true).Handle(serialized(sendHint))
	zero.OnCommand("猜歌放弃").SetBlock(true).Handle(serialized(endGame))
}

func handleGuessSongCommand(ctx *zero.Ctx) {
	args, _ := ctx.State["args"].(string)
	args = strings.TrimSpace(args)
	if args == "" {
		ctx.SendChain(message.Text(usageText))
		return
	}

	fields := strings.Fields(args)
	command := strings.ToLower(fields[0])
	switch command {
	case "jstart":
		startGame(ctx, "rv")
	case "cstart":
		startGame(ctx, "cv")
	case "hint":
		sendHint(ctx)
	case "gg":
		endGame(ctx)
	case "help":
		ctx.SendChain(message.Text(usageText))
	default:
		answerGame(ctx, args)
	}
}

func startGame(ctx *zero.Ctx, library string) {
	work, cancel := context.WithTimeout(context.Background(), prepareTimeout)
	defer cancel()
	id := guessSessionID(ctx)
	session, err := database.LoadGuessGameSession(work, nil, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		ctx.SendChain(message.Text("读取游戏状态失败，请稍后再试。"))
		return
	}
	if err == nil && session.Active {
		ctx.SendChain(message.Text("猜歌进行中，请不要重复使用该命令~"))
		return
	}
	candidates, err := database.ListGuessSongCandidates(work, nil, library)
	if err != nil {
		ctx.SendChain(message.Text("读取题库失败，请稍后再试。"))
		return
	}
	if len(candidates) == 0 {
		ctx.SendChain(message.Text("该题库为空，请先导入歌曲；默认中V题库可使用 .cg cstart。"))
		return
	}
	ctx.SendChain(message.Text("撒，现在请听题！"))
	prepared, err := songs.prepareCandidates(work, id, guessSessionDir(ctx), library, candidates)
	if err != nil {
		log.Warnf("[guesssong] prepare song failed: session=%d library=%s: %v", id, library, err)
		ctx.SendChain(message.Text(prepareSongFailureText(err)))
		return
	}
	segment, err := recordSegmentFromFile(prepared.Slice1Path)
	if err != nil {
		songs.removeSessionFiles(guessSessionDir(ctx))
		log.Warnf("[guesssong] read slice1 failed: %v", err)
		ctx.SendChain(message.Text("读取题目音频失败，请稍后再试。"))
		return
	}
	if !saveSession(ctx, prepared) {
		songs.removeSessionFiles(guessSessionDir(ctx))
		return
	}
	if ctx.SendChain(segment).ID() == 0 {
		if err := database.ClearGuessGameSession(context.Background(), nil, id); err != nil {
			log.Warnf("[guesssong] clear failed delivery: %v", err)
			ctx.SendChain(message.Text("题目发送失败，清理游戏状态也失败，请使用 .cg gg 后重试。"))
			return
		}
		ctx.SendChain(message.Text("题目发送失败，本场已取消，请稍后重试。"))
		songs.removeSessionFiles(guessSessionDir(ctx))
	}
}

func activeSession(ctx *zero.Ctx) *database.GuessGameSession {
	session, err := database.LoadGuessGameSession(context.Background(), nil, guessSessionID(ctx))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		ctx.SendChain(message.Text("读取游戏状态失败，请稍后再试。"))
		return nil
	}
	if err != nil || !session.Active {
		ctx.SendChain(message.Text("猜歌还未开始哦，请使用 .cg cstart / jstart 开始一场猜歌~"))
		return nil
	}
	return session
}

func saveSession(ctx *zero.Ctx, session *database.GuessGameSession) bool {
	if err := database.SaveGuessGameSession(context.Background(), nil, session); err != nil {
		log.Warnf("[guesssong] save session failed: %v", err)
		ctx.SendChain(message.Text("保存游戏状态失败，请稍后再试。"))
		return false
	}
	return true
}

func sendHint(ctx *zero.Ctx) {
	session := activeSession(ctx)
	if session == nil {
		return
	}
	var segment message.Segment
	nextStage := 2
	switch session.HintStage {
	case 0:
		var err error
		segment, err = recordSegmentFromFile(session.Slice2Path)
		if err == nil {
			nextStage = 1
		} else {
			segment = message.Text("第二段音频暂时不可用，直接给你文字提示吧\n这首歌由 " + session.Artist + " 进行演唱/创作。")
		}
	case 1:
		segment = message.Text("这首歌由 " + session.Artist + " 进行演唱/创作。")
	default:
		ctx.SendChain(message.Text("提示机会已用完~"))
		return
	}
	if ctx.SendChain(segment).ID() == 0 {
		ctx.SendChain(message.Text("提示发送失败，请重试。"))
		return
	}
	session.HintStage = nextStage
	saveSession(ctx, session)
}

func endGame(ctx *zero.Ctx) {
	session := activeSession(ctx)
	if session == nil {
		return
	}
	session.Active = false
	if saveSession(ctx, session) {
		songs.removeSessionFiles(guessSessionDir(ctx))
		ctx.SendChain(message.Text(fmt.Sprintf("失败了呢，记好哦，这首歌是《%s》！", session.SongName)))
	}
}

func answerGame(ctx *zero.Ctx, answer string) {
	session := activeSession(ctx)
	if session == nil {
		return
	}
	session.AnswerCount++
	normalized, canonical := normalizeSongName(answer), normalizeSongName(session.Answer)
	var reply string
	switch {
	case normalized != "" && normalized == canonical:
		session.Active = false
		reply = fmt.Sprintf("答对啦，歌曲是《%s》，总计猜了%d次√", session.SongName, session.AnswerCount)
	case normalized != "" && strings.Contains(canonical, normalized):
		reply = "不完全正确！名字里包含你的答案哦！"
	case session.AnswerCount > 3 && session.HintStage < 2:
		reply = "完全没有接近的可能性——需要使用 .cg hint 获取一些提示吗？"
	default:
		reply = "完全没有接近的可能性——"
	}
	if saveSession(ctx, session) {
		if !session.Active {
			songs.removeSessionFiles(guessSessionDir(ctx))
		}
		ctx.SendChain(message.Text(reply))
	}
}

func chooseSliceOffsets(durationMS int) (int, int) {
	duration := durationMS / 1000
	if duration < 2*sliceLengthSec {
		return 0, max(0, duration-sliceLengthSec)
	}
	mid := duration / 2
	return rand.Intn(mid - sliceLengthSec + 1), mid + rand.Intn(duration-mid-sliceLengthSec+1)
}

func recordSegmentFromFile(path string) (message.Segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return message.Segment{}, err
	}
	return message.Record("base64://" + base64.StdEncoding.EncodeToString(data)), nil
}

func normalizeSongTitle(input string) string {
	replacer := strings.NewReplacer("（", "(", "）", ")", "【", "[", "】", "]")
	input = replacer.Replace(strings.TrimSpace(input))
	for {
		updated := stripBracketed(input, '(', ')')
		updated = stripBracketed(updated, '[', ']')
		if updated == input {
			break
		}
		input = strings.TrimSpace(updated)
	}
	return input
}

func stripBracketed(input string, left, right rune) string {
	runes := []rune(input)
	start := -1
	for i, r := range runes {
		if r == left && start == -1 {
			start = i
		}
		if r == right && start >= 0 {
			return string(append(runes[:start], runes[i+1:]...))
		}
	}
	return input
}

func normalizeSongName(input string) string {
	return strings.ToLower(strings.TrimSpace(normalizeSongTitle(input)))
}

func guessSessionID(ctx *zero.Ctx) int64 {
	if ctx != nil && ctx.Event != nil && ctx.Event.GroupID != 0 {
		return ctx.Event.GroupID
	}
	if ctx != nil && ctx.Event != nil && ctx.Event.UserID != 0 {
		return -ctx.Event.UserID
	}
	return 0
}

func guessSessionDir(ctx *zero.Ctx) string {
	if ctx != nil && ctx.Event != nil && ctx.Event.GroupID != 0 {
		return fmt.Sprintf("group_%d", ctx.Event.GroupID)
	}
	if ctx != nil && ctx.Event != nil && ctx.Event.UserID != 0 {
		return fmt.Sprintf("private_%d", ctx.Event.UserID)
	}
	return "unknown"
}

func prepareSongFailureText(err error) string {
	switch {
	case errors.Is(err, errSongDownload):
		return downloadFailureText
	case errors.Is(err, errAudioTools):
		return "准备题目失败，请先安装 ffmpeg 和 ffprobe 后再试。"
	case errors.Is(err, errRateLimited):
		return downloadFailureText
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return downloadFailureText
	case errors.Is(err, errSongUnavailable):
		return downloadFailureText
	default:
		return "准备题目失败，请检查网络、缓存目录及 ffmpeg，并查看服务日志。"
	}
}
