package jrluck

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/imageutil"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const jrrpPicDir = "data/jrrp"

func init() {
	zero.OnCommandGroup([]string{"jrluck", "jrrp", "luck"}).
		SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := strconv.FormatInt(ctx.Event.UserID, 10)
		luckNumber := randomRP(uid)
		luckIndex := randomLuck(uid)

		picPath := luckImagePath(luckIndex)
		absPath, err := filepath.Abs(picPath)
		if err != nil {
			log.Warnf("[jrluck] abs path error: %v", err)
			return
		}

		if _, err := os.Stat(absPath); err != nil {
			sendLuckText(ctx, luckNumber, "")
			return
		}

		img, err := imageutil.LocalImageSegment(absPath)
		if err != nil {
			log.Warnf("[jrluck] load image failed: %v", err)
			sendLuckText(ctx, luckNumber, "")
			return
		}

		text := fmt.Sprintf("今天的人品是%d哦~", luckNumber)

		if ctx.Event.DetailType == "private" {
			ctx.SendChain(message.Text(text+" "), img)
			return
		}
		ctx.SendChain(message.At(ctx.Event.UserID), message.Text(" "+text+" "), img)
	})
}

func sendLuckText(ctx *zero.Ctx, luckNumber int, extraText string) {
	text := fmt.Sprintf("今天的人品是%d哦~", luckNumber)
	if ctx.Event.DetailType == "private" {
		ctx.SendChain(message.Text(text))
		return
	}
	ctx.SendChain(message.At(ctx.Event.UserID), message.Text(" "+text))
}

func randomRP(qq string) int {
	qqInt, _ := strconv.ParseInt(qq, 10, 64)
	dateInt := todayInt()
	seed := qqInt + dateInt + 476800
	r := rand.New(rand.NewSource(seed))
	return r.Intn(101)
}

func randomLuck(qq string) int {
	return computeLuckIndex(qq, isUnluckyTemplate)
}

func computeLuckIndex(qq string, isUnlucky func(int) bool) int {
	qqInt, _ := strconv.ParseInt(qq, 10, 64)
	dateInt := todayInt()
	seed := qqInt + dateInt + 476888
	r := rand.New(rand.NewSource(seed))
	numb := r.Intn(101)

	if numb == 100 {
		return 0
	}

	if isUnlucky != nil && isUnlucky(luckTemplateNumber(numb)) {
		rerollCheck := randomRP(strconv.FormatInt(qqInt+50, 10))
		if rerollCheck > 50 {
			numb = randomRP(strconv.FormatInt(qqInt+10086, 10))
		}
	}

	return numb
}

func isUnluckyTemplate(number int) bool {
	tmpl, err := database.GetDailyLuckTemplate(context.Background(), nil, number)
	if err != nil {
		return false
	}
	return strings.Contains(tmpl.Text, "签 凶")
}

func luckTemplateNumber(luckIndex int) int {
	return luckIndex + 1
}

func luckImagePath(luckIndex int) string {
	return filepath.Join(jrrpPicDir, fmt.Sprintf("%d.jpg", luckIndex))
}

func todayInt() int64 {
	t := time.Now()
	s := fmt.Sprintf("%04d%02d%02d", t.Year(), int(t.Month()), t.Day())
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
