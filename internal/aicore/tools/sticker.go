package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
	"gorm.io/gorm"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

// StickerSearchTool searches for stickers by emotion/scene description.
type StickerSearchTool struct {
	db     *gorm.DB
	client *http.Client
}

func NewStickerSearchTool(db *gorm.DB) *StickerSearchTool {
	return &StickerSearchTool{
		db:     db,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *StickerSearchTool) Name() string { return "sticker_search" }
func (t *StickerSearchTool) Description() string {
	return "搜索表情包/贴纸。根据情感或场景描述查找合适的表情。"
}
func (t *StickerSearchTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"emotion": map[string]interface{}{
				"type":        "string",
				"description": "表情情感，如 happy, sad, angry, surprised",
			},
			"scene": map[string]interface{}{
				"type":        "string",
				"description": "使用场景，如 greeting, farewell, teasing",
			},
		},
	}
}

func (t *StickerSearchTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	emotion, _ := params["emotion"].(string)
	scene, _ := params["scene"].(string)

	q := t.db.WithContext(ctx).Model(&database.Sticker{})
	if emotion != "" {
		q = q.Where("emotion = ?", emotion)
	}
	if scene != "" {
		q = q.Where("scene = ?", scene)
	}

	var stickers []database.Sticker
	if err := q.Limit(5).Find(&stickers).Error; err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("查询表情失败: %v", err)}, nil
	}

	if len(stickers) == 0 {
		return &tool.ToolResult{Success: true, Output: "没有找到匹配的表情"}, nil
	}

	var sb strings.Builder
	sb.WriteString("找到以下表情:\n")
	for i, s := range stickers {
		sb.WriteString(fmt.Sprintf("%d. [%s] 情感:%s 意图:%s 场景:%s 文件:%s\n",
			i+1, s.FileHash, s.Emotion, s.Intent, s.Scene, s.FilePath))
	}
	return &tool.ToolResult{Success: true, Output: sb.String()}, nil
}

// StickerAnalyzeTool analyzes a sticker image using LLM.
type StickerAnalyzeTool struct {
	db     *gorm.DB
	cfg    *config.Config
	recall *MemoryRecallTool // reuse embedding
}

func NewStickerAnalyzeTool(db *gorm.DB, cfg *config.Config) *StickerAnalyzeTool {
	return &StickerAnalyzeTool{
		db:  db,
		cfg: cfg,
		recall: &MemoryRecallTool{
			db:     db,
			cfg:    cfg,
			client: defaultHTTPClient(),
		},
	}
}

func (t *StickerAnalyzeTool) Name() string { return "sticker_analyze" }
func (t *StickerAnalyzeTool) Description() string {
	return "分析并存储表情包/贴纸的含义。当收到新的表情包时使用，提取情感和意图。"
}
func (t *StickerAnalyzeTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"file_hash": map[string]interface{}{
				"type":        "string",
				"description": "表情文件哈希",
			},
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "表情文件路径或URL",
			},
			"emotion": map[string]interface{}{
				"type":        "string",
				"description": "识别出的情感: happy, sad, angry, surprised, disgusted, neutral",
			},
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "识别出的意图: teasing, caring, greeting, farewell, encouraging",
			},
			"scene": map[string]interface{}{
				"type":        "string",
				"description": "适用场景描述",
			},
		},
		Required: []string{"file_hash", "emotion", "intent"},
	}
}

func (t *StickerAnalyzeTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	fileHash, _ := params["file_hash"].(string)
	filePath, _ := params["file_path"].(string)
	emotion, _ := params["emotion"].(string)
	intent, _ := params["intent"].(string)
	scene, _ := params["scene"].(string)

	if fileHash == "" {
		return &tool.ToolResult{Success: false, Output: "file_hash 不能为空"}, nil
	}

	// Check if already exists
	var existing database.Sticker
	if t.db.Where("file_hash = ?", fileHash).First(&existing).Error == nil {
		return &tool.ToolResult{Success: true, Output: "该表情已存在于数据库中"}, nil
	}

	// Get embedding for the description
	desc := fmt.Sprintf("%s %s %s", emotion, intent, scene)
	embedding, err := t.recall.getEmbedding(ctx, desc)
	if err != nil {
		// Store without embedding if it fails
		embedding = make([]float32, t.cfg.Embedding.Dimensions)
	}

	sticker := database.Sticker{
		FileHash:  fileHash,
		FilePath:  filePath,
		Emotion:   emotion,
		Intent:    intent,
		Scene:     scene,
		Embedding: pgvector.NewVector(embedding),
	}
	if err := t.db.Create(&sticker).Error; err != nil {
		return &tool.ToolResult{Success: false, Output: fmt.Sprintf("存储表情失败: %v", err)}, nil
	}

	return &tool.ToolResult{Success: true, Output: "表情已分析并存储"}, nil
}
