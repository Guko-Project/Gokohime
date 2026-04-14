package eatwhat

import (
	"fmt"
	"strings"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"
)

const usageText = `【今天吃什么】
.eat - 获取正经饭饭推荐
.eats - 获取校内食堂推荐
.eatwhat / .吃什么 - 获取不正经的饭饭
.eadd 内容 - 添加一条豪华菜单
.edel 内容 - 删除一条豪华菜单`

func init() {
	rand.Seed(time.Now().UnixNano())

	if err := ensureMealDataFile(); err != nil {
		panic(fmt.Errorf("[eatwhat] init data file: %w", err))
	}

	zero.OnCommandGroup([]string{"eat", "eats", "eatwhat", "吃什么"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			kind := commandToMealKind(ctx.State["command"])
			meal, err := getRandomMeal(kind)
			if err != nil {
				ctx.SendChain(message.Text("菜单加载失败了，请稍后再试~"))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("今天去吃%s吧！", meal)))
		})

	zero.OnCommandGroup([]string{"eadd"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			meal := extractArgs(ctx)
			if meal == "" {
				ctx.SendChain(message.Text(usageText))
				return
			}

			exists, err := checkMealInList("eatwhat", meal)
			if err != nil {
				ctx.SendChain(message.Text("菜单加载失败了，请稍后再试~"))
				return
			}
			if exists {
				ctx.SendChain(message.Text(fmt.Sprintf("%s已经在今日豪华菜单里了哦！", meal)))
				return
			}

			if err := addMeal("eatwhat", meal); err != nil {
				ctx.SendChain(message.Text("添加失败了，请稍后再试~"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("%s已添加到今日豪华菜单！", meal)))
		})

	zero.OnCommandGroup([]string{"edel"}).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			meal := extractArgs(ctx)
			if meal == "" {
				ctx.SendChain(message.Text(usageText))
				return
			}

			exists, err := checkMealInList("eatwhat", meal)
			if err != nil {
				ctx.SendChain(message.Text("菜单加载失败了，请稍后再试~"))
				return
			}
			if !exists {
				ctx.SendChain(message.Text(fmt.Sprintf("%s没有在今日豪华菜单里哦...", meal)))
				return
			}

			if err := delMeal("eatwhat", meal); err != nil {
				ctx.SendChain(message.Text("删除失败了，请稍后再试~"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("%s已从今日豪华菜单中删除！", meal)))
		})
}

func commandToMealKind(command any) string {
	cmd, _ := command.(string)
	switch cmd {
	case "eat":
		return "eat"
	case "eats":
		return "eats"
	case "吃什么", "eatwhat":
		fallthrough
	default:
		return "eatwhat"
	}
}

func extractArgs(ctx *zero.Ctx) string {
	if ctx == nil {
		return ""
	}

	args, _ := ctx.State["args"].(string)
	return strings.TrimSpace(args)
}
