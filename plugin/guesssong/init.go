package guesssong

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/database"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const (
	usageText      = "猜歌说明：\n使用 .cg jstart 开始日V猜歌\n使用 .cg cstart 开始中V猜歌\n使用 .cg hint 获取帮助，第一次是另一段歌曲，第二次是P主/演唱者名称\n使用 .cg gg 放弃猜歌\n使用 .cg 歌名 做出回答\n使用 .cg help 获取本帮助信息"
	sliceLengthSec = 6
)

var guessSongClient = &http.Client{Timeout: 30 * time.Second}

func init() {
	zero.OnCommand("cg").SetBlock(true).Handle(handleGuessSongCommand)
	zero.OnCommand("猜歌").SetBlock(true).Handle(func(ctx *zero.Ctx) {
		startGame(ctx, "rv")
	})
	zero.OnCommand("猜歌提示").SetBlock(true).Handle(sendHint)
	zero.OnCommand("猜歌放弃").SetBlock(true).Handle(endGame)
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
	sessionID := guessSessionID(ctx)
	session, err := database.LoadGuessGameSession(context.Background(), nil, sessionID)
	if err == nil && session.Active {
		ctx.SendChain(message.Text("猜歌进行中，请不要重复使用该命令~"))
		return
	}

	catalog, err := database.RandomGuessSongCatalog(context.Background(), nil, library)
	if err != nil {
		ctx.SendChain(message.Text("题库为空，请先运行迁移脚本导入猜歌数据"))
		return
	}

	prepared, err := prepareSong(sessionID, guessSessionDir(ctx), library, catalog.SongID)
	if err != nil {
		log.Warnf("[guesssong] prepare song failed: %v", err)
		ctx.SendChain(message.Text(prepareSongFailureText(err)))
		return
	}
	if err := database.SaveGuessGameSession(context.Background(), nil, prepared); err != nil {
		log.Warnf("[guesssong] save session failed: %v", err)
		ctx.SendChain(message.Text("保存游戏状态失败"))
		return
	}

	segment, err := recordSegmentFromFile(prepared.Slice1Path)
	if err != nil {
		log.Warnf("[guesssong] send slice1 failed: %v", err)
		ctx.SendChain(message.Text("题目音频生成失败，请检查 ffmpeg 是否可用。"))
		return
	}
	ctx.SendChain(segment)
}

func sendHint(ctx *zero.Ctx) {
	session, err := database.LoadGuessGameSession(context.Background(), nil, guessSessionID(ctx))
	if err != nil || !session.Active {
		ctx.SendChain(message.Text("猜歌还未开始哦，请使用 .cg cstart / jstart 开始一场猜歌~"))
		return
	}

	switch session.HintStage {
	case 0:
		segment, err := recordSegmentFromFile(session.Slice2Path)
		if err != nil {
			ctx.SendChain(message.Text("第二段音频暂时不可用，直接给你文字提示吧\n这首歌由 " + session.Artist + " 进行演唱/创作。"))
			session.HintStage = 2
		} else {
			ctx.SendChain(segment)
			session.HintStage = 1
		}
	case 1:
		ctx.SendChain(message.Text("这首歌由 " + session.Artist + " 进行演唱/创作。"))
		session.HintStage = 2
	default:
		ctx.SendChain(message.Text("提示机会已用完~"))
		return
	}

	_ = database.SaveGuessGameSession(context.Background(), nil, session)
}

func endGame(ctx *zero.Ctx) {
	session, err := database.LoadGuessGameSession(context.Background(), nil, guessSessionID(ctx))
	if err != nil || !session.Active {
		ctx.SendChain(message.Text("当前没有进行中的猜歌游戏"))
		return
	}
	session.Active = false
	_ = database.SaveGuessGameSession(context.Background(), nil, session)
	ctx.SendChain(message.Text(fmt.Sprintf("失败了呢，记好哦，这首歌是《%s》！", session.SongName)))
}

func answerGame(ctx *zero.Ctx, answer string) {
	session, err := database.LoadGuessGameSession(context.Background(), nil, guessSessionID(ctx))
	if err != nil || !session.Active {
		ctx.SendChain(message.Text("猜歌还未开始哦，请使用 .cg cstart / jstart 开始一场猜歌~"))
		return
	}

	session.AnswerCount++
	normalized := normalizeSongName(answer)
	canonical := normalizeSongName(session.Answer)

	switch {
	case normalized == canonical:
		session.Active = false
		_ = database.SaveGuessGameSession(context.Background(), nil, session)
		ctx.SendChain(message.Text(fmt.Sprintf("答对啦，歌曲是《%s》，总计猜了%d次√", session.SongName, session.AnswerCount)))
	case normalized != "" && strings.Contains(canonical, normalized):
		_ = database.SaveGuessGameSession(context.Background(), nil, session)
		ctx.SendChain(message.Text("不完全正确！名字里包含你的答案哦！"))
	case session.AnswerCount > 3 && session.HintStage < 2:
		_ = database.SaveGuessGameSession(context.Background(), nil, session)
		ctx.SendChain(message.Text("完全没有接近的可能性——需要使用 .cg hint 获取一些提示吗？"))
	default:
		_ = database.SaveGuessGameSession(context.Background(), nil, session)
		ctx.SendChain(message.Text("完全没有接近的可能性——"))
	}
}

func prepareSong(sessionID int64, sessionDir, library string, songID int64) (*database.GuessGameSession, error) {
	info, err := fetchSongInfo(songID)
	if err != nil {
		return nil, err
	}

	saveDir := filepath.Join("data", "guesssong", sessionDir)
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return nil, err
	}

	tempPath := filepath.Join(saveDir, "temp.mp3")
	if err := downloadFile(info.AudioURL, tempPath); err != nil {
		return nil, err
	}

	slice1Path := filepath.Join(saveDir, "slice_1.wav")
	slice2Path := filepath.Join(saveDir, "slice_2.wav")
	start1, start2 := chooseSliceOffsets(info.DurationMS)
	if err := cutAudio(tempPath, slice1Path, start1); err != nil {
		return nil, err
	}
	if err := cutAudio(tempPath, slice2Path, start2); err != nil {
		return nil, err
	}

	return &database.GuessGameSession{
		GroupID:     sessionID,
		Library:     library,
		SongID:      songID,
		SongName:    info.Name,
		Artist:      info.Artist,
		AudioURL:    info.AudioURL,
		Slice1Path:  slice1Path,
		Slice2Path:  slice2Path,
		HintStage:   0,
		AnswerCount: 0,
		Answer:      info.Name,
		PreparedAt:  time.Now(),
		Active:      true,
		LastError:   "",
	}, nil
}

type songInfo struct {
	Name       string
	Artist     string
	AudioURL   string
	DurationMS int
}

func fetchSongInfo(songID int64) (*songInfo, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://music.163.com/api/song/detail/?ids=[%d]", songID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := guessSongClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Songs []struct {
			Name string `json:"name"`
			DT   int    `json:"dt"`
			Ar   []struct {
				Name string `json:"name"`
			} `json:"ar"`
		} `json:"songs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Songs) == 0 {
		return nil, fmt.Errorf("song %d not found", songID)
	}

	name := normalizeSongTitle(payload.Songs[0].Name)
	artist := ""
	if len(payload.Songs[0].Ar) > 0 {
		artist = payload.Songs[0].Ar[0].Name
	}

	return &songInfo{
		Name:       name,
		Artist:     artist,
		AudioURL:   fmt.Sprintf("https://music.163.com/song/media/outer/url?id=%d.mp3", songID),
		DurationMS: payload.Songs[0].DT,
	}, nil
}

func downloadFile(url, path string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := guessSongClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download audio status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func chooseSliceOffsets(durationMS int) (int, int) {
	durationSec := durationMS / 1000
	if durationSec <= sliceLengthSec {
		return 0, 0
	}

	firstMax := maxInt(0, durationSec/2-sliceLengthSec)
	secondMin := maxInt(sliceLengthSec, durationSec/2)
	secondMax := maxInt(secondMin, durationSec-sliceLengthSec)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	start1 := 0
	if firstMax > 0 {
		start1 = r.Intn(firstMax + 1)
	}

	start2 := secondMin
	if secondMax > secondMin {
		start2 = secondMin + r.Intn(secondMax-secondMin+1)
	}
	return start1, start2
}

func cutAudio(inputPath, outputPath string, startSec int) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	cmd := exec.Command("ffmpeg", "-y", "-ss", fmt.Sprintf("%d", startSec), "-t", fmt.Sprintf("%d", sliceLengthSec), "-i", inputPath, outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "ffmpeg not found"):
		return "准备题目失败，请先安装 ffmpeg 后再试。"
	case strings.Contains(msg, "ffmpeg:"):
		return "准备题目失败，ffmpeg 切片失败，请检查音频文件和 ffmpeg 是否可用。"
	case strings.Contains(msg, "download audio status"), strings.Contains(msg, "song "), strings.Contains(msg, "not found"):
		return "准备题目失败，歌曲资源暂时不可用，请稍后再试。"
	default:
		return "准备题目失败，请确认网络可用且系统已安装 ffmpeg。"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
