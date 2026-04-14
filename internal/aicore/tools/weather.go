package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/tool"

	"github.com/colanns/gokohime/internal/config"
)

type WeatherTool struct {
	apiKey      string
	defaultCity string
	cacheTTL    time.Duration
	cache       sync.Map
	client      *http.Client
}

type weatherCache struct {
	data      string
	timestamp time.Time
}

func NewWeatherTool(cfg *config.Config) *WeatherTool {
	return &WeatherTool{
		apiKey:      cfg.Weather.APIKey,
		defaultCity: cfg.Weather.DefaultCity,
		cacheTTL:    time.Duration(cfg.Weather.CacheTTLMin) * time.Minute,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *WeatherTool) Name() string { return "get_weather" }
func (t *WeatherTool) Description() string {
	return "获取指定城市的当前天气信息。当用户询问天气、温度、是否需要带伞等问题时使用。"
}
func (t *WeatherTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"city": map[string]interface{}{
				"type":        "string",
				"description": "城市名称或高德城市代码，如'北京'、'上海'、'110000'。不确定时可留空使用默认城市。",
			},
		},
	}
}

func (t *WeatherTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	city, _ := params["city"].(string)
	if city == "" {
		city = t.defaultCity
	}
	if t.apiKey == "" {
		return &tool.ToolResult{Success: false, Output: "天气服务未配置"}, nil
	}

	// Check cache
	cacheKey := city
	if cached, ok := t.cache.Load(cacheKey); ok {
		wc := cached.(*weatherCache)
		if time.Since(wc.timestamp) < t.cacheTTL {
			return &tool.ToolResult{Success: true, Output: wc.data}, nil
		}
	}

	// Fetch from Amap API
	url := fmt.Sprintf("https://restapi.amap.com/v3/weather/weatherInfo?key=%s&city=%s&extensions=base", t.apiKey, city)
	resp, err := t.client.Get(url)
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("请求天气API失败: %v", err)}, nil
	}
	defer resp.Body.Close()

	var result struct {
		Status string `json:"status"`
		Lives  []struct {
			Province     string `json:"province"`
			City         string `json:"city"`
			Weather      string `json:"weather"`
			Temperature  string `json:"temperature"`
			WindDirection string `json:"winddirection"`
			WindPower    string `json:"windpower"`
			Humidity     string `json:"humidity"`
			ReportTime   string `json:"reporttime"`
		} `json:"lives"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("解析天气数据失败: %v", err)}, nil
	}

	if result.Status != "1" || len(result.Lives) == 0 {
		return &tool.ToolResult{Success: false, Output: "未找到该城市的天气信息"}, nil
	}

	w := result.Lives[0]
	output := fmt.Sprintf("%s%s: %s, 温度%s°C, %s风%s级, 湿度%s%%, 更新时间: %s",
		w.Province, w.City, w.Weather, w.Temperature,
		w.WindDirection, w.WindPower, w.Humidity, w.ReportTime)

	// Update cache
	t.cache.Store(cacheKey, &weatherCache{data: output, timestamp: time.Now()})

	return &tool.ToolResult{Success: true, Output: output}, nil
}
