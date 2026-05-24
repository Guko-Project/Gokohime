package chp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var client = &http.Client{Timeout: 10 * time.Second}

const chpAPI = "https://api.shadiao.pro/chp"

func init() {
	zero.OnCommand("chp").SetBlock(true).Handle(handleCHP)
}

func handleCHP(ctx *zero.Ctx) {
	text, err := fetchCHP()
	if err != nil {
		ctx.SendChain(message.Text("查询失败，" + err.Error()))
		return
	}
	ctx.SendChain(message.Text(text))
}

func fetchCHP() (string, error) {
	return fetchCHPFrom(client, chpAPI)
}

func fetchCHPFrom(httpClient *http.Client, endpoint string) (string, error) {
	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("接口状态码 %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	text := strings.TrimSpace(payload.Data.Text)
	if text == "" {
		return "", errors.New("接口没有返回内容")
	}
	return text, nil
}
