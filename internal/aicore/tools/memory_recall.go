package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

type MemoryRecallTool struct {
	db     *gorm.DB
	cfg    *config.Config
	client *http.Client
}

func NewMemoryRecallTool(db *gorm.DB, cfg *config.Config) *MemoryRecallTool {
	return &MemoryRecallTool{
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *MemoryRecallTool) Name() string { return "memory_recall" }
func (t *MemoryRecallTool) Description() string {
	return "回忆与当前话题相关的历史对话和用户信息。当需要参考之前的对话内容或用户偏好时使用。"
}
func (t *MemoryRecallTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "要回忆的内容描述，如'用户喜欢什么'、'之前讨论过的话题'",
			},
			"group_id": map[string]interface{}{
				"type":        "integer",
				"description": "群号，用于限定搜索范围",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "返回的最相关记忆数量，默认5",
			},
		},
		Required: []string{"query"},
	}
}

func (t *MemoryRecallTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return &tool.ToolResult{Success: false, Output: "查询内容不能为空"}, nil
	}

	groupID := int64(0)
	if gid, ok := params["group_id"].(float64); ok {
		groupID = int64(gid)
	}
	topK := 5
	if k, ok := params["top_k"].(float64); ok && int(k) > 0 {
		topK = int(k)
	}

	// Get embedding for query
	embedding, err := t.getEmbedding(ctx, query)
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("获取embedding失败: %v", err)}, nil
	}

	// Search memories by vector similarity
	var memories []database.Memory
	q := t.db.WithContext(ctx)
	if groupID > 0 {
		q = q.Where("group_id = ?", groupID)
	}
	err = q.Clauses(clause.OrderBy{
		Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []interface{}{pgvector.NewVector(embedding)}},
	}).Limit(topK).Find(&memories).Error
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("查询记忆失败: %v", err)}, nil
	}

	if len(memories) == 0 {
		return &tool.ToolResult{Success: true, Output: "没有找到相关记忆"}, nil
	}

	// Also fetch user profiles
	var profiles []database.UserProfile
	if groupID > 0 {
		t.db.Where("group_id = ?", groupID).Find(&profiles)
	}

	var sb strings.Builder
	sb.WriteString("找到以下相关记忆:\n")
	for i, m := range memories {
		sb.WriteString(fmt.Sprintf("%d. [群%d] %s\n", i+1, m.GroupID, m.Content))
	}
	if len(profiles) > 0 {
		sb.WriteString("\n相关用户信息:\n")
		for _, p := range profiles {
			sb.WriteString(fmt.Sprintf("- %s (QQ:%d): %s\n", p.Name, p.UserID, p.Details))
		}
	}

	return &tool.ToolResult{Success: true, Output: sb.String()}, nil
}

func (t *MemoryRecallTool) getEmbedding(ctx context.Context, text string) ([]float32, error) {
	apiKey := t.cfg.AI.OpenAIAPIKey
	if apiKey == "" {
		apiKey = t.cfg.AI.AnthropicAPIKey
	}
	apiBase := t.cfg.AI.OpenAIAPIBase
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}

	reqBody := map[string]interface{}{
		"model":      t.cfg.Embedding.Model,
		"input":      text,
		"dimensions": t.cfg.Embedding.Dimensions,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", apiBase+"/embeddings", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return result.Data[0].Embedding, nil
}
