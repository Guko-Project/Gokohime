package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/tool"

	"github.com/colanns/gokohime/internal/config"
)

type WebSearchTool struct {
	apiKey  string
	apiBase string
	model   string
	client  *http.Client
}

func NewWebSearchTool(cfg *config.Config) *WebSearchTool {
	return &WebSearchTool{
		apiKey:  cfg.Search.APIKey,
		apiBase: cfg.Search.APIBase,
		model:   cfg.Search.Model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "搜索互联网获取最新信息。当用户询问最新新闻、实时信息、或你不确定的事实时使用。"
}
func (t *WebSearchTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词",
			},
		},
		Required: []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return &tool.ToolResult{Success: false, Output: "搜索关键词不能为空"}, nil
	}
	if t.apiKey == "" {
		return &tool.ToolResult{Success: false, Output: "搜索服务未配置"}, nil
	}

	// Call search API (Doubao/OpenAI-compatible format)
	reqBody := map[string]interface{}{
		"model": t.model,
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
		"max_tokens": 1024,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	apiURL := t.apiBase + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("创建请求失败: %v", err)}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("搜索请求失败: %v", err)}, nil
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("解析搜索结果失败: %v", err)}, nil
	}

	if len(result.Choices) == 0 {
		return &tool.ToolResult{Success: false, Output: "搜索无结果"}, nil
	}

	return &tool.ToolResult{Success: true, Output: result.Choices[0].Message.Content}, nil
}
