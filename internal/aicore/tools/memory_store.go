package tools

import (
	"context"
	"fmt"
	nethttp "net/http"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
	"gorm.io/gorm"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

type MemoryStoreTool struct {
	db     *gorm.DB
	cfg    *config.Config
	recall *MemoryRecallTool // reuse embedding logic
}

func NewMemoryStoreTool(db *gorm.DB, cfg *config.Config) *MemoryStoreTool {
	return &MemoryStoreTool{
		db:  db,
		cfg: cfg,
		recall: &MemoryRecallTool{
			db:     db,
			cfg:    cfg,
			client: defaultHTTPClient(),
		},
	}
}

func (t *MemoryStoreTool) Name() string { return "memory_store" }
func (t *MemoryStoreTool) Description() string {
	return "存储重要的用户信息或对话事实到长期记忆。当用户透露个人偏好、重要事件、或有价值的信息时使用。"
}
func (t *MemoryStoreTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "要记忆的内容，用简洁的陈述句描述事实",
			},
			"user_id": map[string]interface{}{
				"type":        "integer",
				"description": "相关用户的QQ号",
			},
			"group_id": map[string]interface{}{
				"type":        "integer",
				"description": "群号",
			},
			"user_name": map[string]interface{}{
				"type":        "string",
				"description": "用户昵称（如果已知）",
			},
		},
		Required: []string{"content"},
	}
}

func (t *MemoryStoreTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	content, _ := params["content"].(string)
	if content == "" {
		return &tool.ToolResult{Success: false, Output: "记忆内容不能为空"}, nil
	}

	userID := int64(0)
	if uid, ok := params["user_id"].(float64); ok {
		userID = int64(uid)
	}
	groupID := int64(0)
	if gid, ok := params["group_id"].(float64); ok {
		groupID = int64(gid)
	}
	userName, _ := params["user_name"].(string)

	// Get embedding for the content
	embedding, err := t.recall.getEmbedding(ctx, content)
	if err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("生成embedding失败: %v", err)}, nil
	}

	// Store memory
	memory := database.Memory{
		UserID:    userID,
		GroupID:   groupID,
		Content:   content,
		Embedding: pgvector.NewVector(embedding),
		Scope:     "group",
	}
	if err := t.db.Create(&memory).Error; err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("存储记忆失败: %v", err)}, nil
	}

	// Update user profile if we have user info
	if userID > 0 && userName != "" {
		var profile database.UserProfile
		result := t.db.Where("user_id = ? AND group_id = ?", userID, groupID).First(&profile)
		if result.Error != nil {
			// Create new profile
			profile = database.UserProfile{
				UserID:  userID,
				GroupID: groupID,
				Name:    userName,
				Details: content,
			}
			t.db.Create(&profile)
		} else {
			// Append to existing details
			t.db.Model(&profile).Update("details", profile.Details+"\n"+content)
		}
	}

	return &tool.ToolResult{Success: true, Output: "已记住"}, nil
}

func defaultHTTPClient() *nethttp.Client {
	return &nethttp.Client{Timeout: 15 * time.Second}
}
