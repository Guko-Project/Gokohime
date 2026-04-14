package randpic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/commandutil"
	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/imageutil"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const dataDir = "data/randpic"

var (
	httpClient = &http.Client{Timeout: 20 * time.Second}

	startupCheckMu   sync.Mutex
	startupCheckDone bool

	legacySayingAliases = map[string]string{
		"fu":     "fu",
		"fufu":   "fu",
		"gst":    "gst",
		"gu":     "gu",
		"kira":   "kira",
		"motohg": "motohg",
		"pjsk":   "pjsk",
		"rui":    "rui",
		"wt":     "wt",
		"tls":    "tls",
	}
)

func init() {
	zero.OnCommandGroup([]string{"随机图片", "randpic", "pic"}).
		SetBlock(true).
		Handle(handleGeneralRandPicCommand)

	zero.OnMessage(commandutil.ReplyableCommandRule("add")).
		SetBlock(true).
		Handle(handleAddRandPicCommand)

	// Direct category commands such as .dly / .kgk / .gst / .fu.
	zero.OnMessage().
		SetPriority(10).
		SetBlock(false).
		Handle(handleDirectCategoryCommand)
}

func handleGeneralRandPicCommand(ctx *zero.Ctx) {
	if isRandPicDisabledInCurrentChat(ctx) {
		return
	}
	if !isExactCommandInvocation(ctx) {
		return
	}
	warmRandPicIndex()

	args, _ := ctx.State["args"].(string)
	category := normalizeCategory(firstField(args))
	if category == "" {
		sendCategoryList(ctx)
		return
	}

	item, err := database.RandomRandPicItem(context.Background(), nil, category)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.SendChain(message.Text("分类 " + category + " 不存在"))
			return
		}
		ctx.SendChain(message.Text("图片读取失败了，请稍后再试~"))
		return
	}

	sendRandPicItem(ctx, item, "图片发送失败")
}

func handleAddRandPicCommand(ctx *zero.Ctx) {
	if isRandPicDisabledInCurrentChat(ctx) {
		return
	}
	if !isExactCommandInvocation(ctx) {
		return
	}
	warmRandPicIndex()

	args, _ := ctx.State["args"].(string)
	category := normalizeCategory(firstField(args))
	if category == "" {
		ctx.SendChain(message.Text("用法: .add <分类> 并附带图片"))
		return
	}
	if !isUploadableCategory(category) {
		ctx.SendChain(message.Text("分类不存在或当前不支持上传"))
		return
	}

	imageURLs := extractImageURLs(ctx.Event.Message)
	if len(imageURLs) == 0 {
		imageURLs = extractImageURLs(extractReplyMessage(ctx))
	}
	if len(imageURLs) == 0 {
		ctx.SendChain(message.Text("请在消息里附带要保存的图片，或回复一张图片后使用 .add 或 /add"))
		return
	}

	saved := 0
	duplicates := 0
	for _, url := range imageURLs {
		ok, err := saveImage(category, url)
		if err != nil {
			log.Warnf("[randpic] save image failed: %v", err)
			continue
		}
		if ok {
			saved++
		} else {
			duplicates++
		}
	}

	switch {
	case saved == 0 && duplicates > 0:
		ctx.SendChain(message.Text("导入失败，这些图片已经存在于该分类中"))
	case saved == 0:
		ctx.SendChain(message.Text("导入失败，可能是图片下载失败"))
	case duplicates > 0:
		ctx.SendChain(message.Text(fmt.Sprintf("导入成功，共保存 %d 张图片到 %s，另有 %d 张重复图片已跳过", saved, category, duplicates)))
	default:
		ctx.SendChain(message.Text(fmt.Sprintf("导入成功，共保存 %d 张图片到 %s", saved, category)))
	}
}

func handleDirectCategoryCommand(ctx *zero.Ctx) {
	if isRandPicDisabledInCurrentChat(ctx) {
		return
	}
	command, args, ok := parsePrefixedCommand(ctx)
	if !ok {
		return
	}
	category, ok := lookupDirectCategoryCommand(command)
	if !ok {
		return
	}
	warmRandPicIndex()

	if command == "fu" && strings.TrimSpace(args) != "" {
		item, err := database.SearchRandPicItem(context.Background(), nil, category, args)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				ctx.SendChain(message.Text("未获取到该语录..."))
				ctx.Block()
				return
			}
			ctx.SendChain(message.Text("语录读取失败了，请稍后再试~"))
			ctx.Block()
			return
		}
		sendRandPicItem(ctx, item, "语录图片发送失败")
		ctx.Block()
		return
	}

	item, err := database.RandomRandPicItem(context.Background(), nil, category)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if isLegacySayingCommand(command) {
				ctx.SendChain(message.Text("未获取到该语录..."))
			} else {
				ctx.SendChain(message.Text("当前还没有图片!"))
			}
			ctx.Block()
			return
		}
		ctx.SendChain(message.Text("图片读取失败了，请稍后再试~"))
		ctx.Block()
		return
	}

	errText := "图片发送失败"
	if isLegacySayingCommand(command) {
		errText = "语录图片发送失败"
	}
	sendRandPicItem(ctx, item, errText)
	ctx.Block()
}

func lookupDirectCategoryCommand(command string) (string, bool) {
	aliases := directCommandAliases()
	category, ok := aliases[strings.ToLower(strings.TrimSpace(command))]
	return category, ok
}

func sendCategoryList(ctx *zero.Ctx) {
	warmRandPicIndex()

	categories, err := database.DistinctRandPicCategories(context.Background(), nil)
	if err != nil || len(categories) == 0 {
		categories = sortedKeys(readableCategorySet())
	}
	if len(categories) == 0 {
		ctx.SendChain(message.Text("暂无图片分类"))
		return
	}
	ctx.SendChain(message.Text("可用分类: " + strings.Join(categories, ", ")))
}

func sendRandPicItem(ctx *zero.Ctx, item *database.RandPicItem, errText string) {
	if item == nil {
		ctx.SendChain(message.Text(errText))
		return
	}

	absPath, err := filepath.Abs(item.FilePath)
	if err != nil {
		log.Warnf("[randpic] abs path error: %v", err)
		ctx.SendChain(message.Text(errText))
		return
	}

	img, err := imageutil.LocalImageSegment(absPath)
	if err != nil {
		log.Warnf("[randpic] load image failed: %v", err)
		ctx.SendChain(message.Text(errText))
		return
	}
	ctx.SendChain(img)
}

func extractImageURLs(segments message.Message) []string {
	var urls []string
	for _, seg := range segments {
		if seg.Type != "image" {
			continue
		}
		if url := seg.Data["url"]; url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}

func extractReplyMessage(ctx *zero.Ctx) message.Message {
	if ctx == nil || ctx.Event == nil {
		return nil
	}

	for _, seg := range ctx.Event.Message {
		if seg.Type != "reply" || seg.Data["id"] == "" {
			continue
		}
		msg := ctx.GetMessage(seg.Data["id"])
		return msg.Elements
	}

	return nil
}

func saveImage(category, imageURL string) (bool, error) {
	resp, err := httpClient.Get(imageURL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	var count int64
	if err := database.Get().
		Model(&database.RandPicItem{}).
		Where("category = ? AND file_hash = ?", category, hash).
		Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	ext := detectExt(resp.Header.Get("Content-Type"), imageURL)
	dir := filepath.Join(randPicBaseDir(), category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}

	fileName := fmt.Sprintf("randpic_%s_%s%s", category, hash[:8], ext)
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}

	entry := &database.RandPicItem{
		Category: category,
		FileName: fileName,
		FilePath: filepath.ToSlash(path),
		FileHash: hash,
	}
	if err := database.Get().
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "category"},
				{Name: "file_hash"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"file_name", "file_path", "updated_at"}),
		}).
		Create(entry).Error; err != nil {
		return false, err
	}
	return true, nil
}

func detectExt(contentType, imageURL string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	}
	ext := strings.ToLower(filepath.Ext(strings.Split(imageURL, "?")[0]))
	switch ext {
	case ".png", ".gif", ".webp", ".jpg", ".jpeg":
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	default:
		return ".jpg"
	}
}

func directCommandAliases() map[string]string {
	aliases := make(map[string]string)
	for _, category := range availableRandPicCategories() {
		aliases[strings.ToLower(category)] = strings.ToLower(category)
	}
	for alias, category := range legacySayingAliases {
		aliases[alias] = category
	}
	return aliases
}

func readableCategorySet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, category := range availableRandPicCategories() {
		set[strings.ToLower(category)] = struct{}{}
	}
	for _, category := range legacySayingAliases {
		set[category] = struct{}{}
	}
	return set
}

func configuredRandPicCategories() []string {
	set := make(map[string]struct{})
	if cfg := config.Get(); cfg != nil {
		for _, item := range cfg.RandPic.CommandList {
			item = strings.ToLower(strings.TrimSpace(item))
			if item != "" {
				set[item] = struct{}{}
			}
		}
	}

	if entries, err := os.ReadDir(randPicBaseDir()); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(entry.Name()))
			if name == "" {
				continue
			}
			set[name] = struct{}{}
		}
	}

	return sortedKeys(set)
}

func availableRandPicCategories() []string {
	categories, err := database.DistinctRandPicCategories(context.Background(), nil)
	if err == nil && len(categories) > 0 {
		return categories
	}
	return configuredRandPicCategories()
}

func isUploadableCategory(category string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" || isLegacySayingCategory(category) {
		return false
	}
	for _, item := range configuredRandPicCategories() {
		if item == category {
			return true
		}
	}
	return false
}

func isLegacySayingCommand(command string) bool {
	_, ok := legacySayingAliases[strings.ToLower(command)]
	return ok
}

func isLegacySayingCategory(category string) bool {
	for _, item := range legacySayingAliases {
		if item == category {
			return true
		}
	}
	return false
}

func parsePrefixedCommand(ctx *zero.Ctx) (string, string, bool) {
	if ctx == nil || ctx.Event == nil {
		return "", "", false
	}

	prefix := "."
	if cfg := config.Get(); cfg != nil && strings.TrimSpace(cfg.Bot.CommandPrefix) != "" {
		prefix = cfg.Bot.CommandPrefix
	}

	plain := strings.TrimSpace(ctx.ExtractPlainText())
	if !strings.HasPrefix(plain, prefix) {
		return "", "", false
	}

	trimmed := strings.TrimSpace(strings.TrimPrefix(plain, prefix))
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", "", false
	}

	command := strings.ToLower(fields[0])
	args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	return command, args, true
}

func isExactCommandInvocation(ctx *zero.Ctx) bool {
	if ctx == nil || ctx.Event == nil {
		return false
	}

	matched, _ := ctx.State["command"].(string)
	if matched == "" {
		return false
	}

	match, ok := commandutil.MatchReplyableCommand(ctx.Event.Message, matched)
	if !ok {
		return false
	}
	return match.Command == strings.ToLower(strings.TrimSpace(matched))
}

func normalizeCategory(category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" {
		return ""
	}
	if mapped, ok := directCommandAliases()[category]; ok {
		return mapped
	}
	return category
}

func firstField(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// StartupCheck indexes local randpic assets into the database so legacy files
// remain directly usable even before a full migration script runs.
func StartupCheck(ctx context.Context) error {
	startupCheckMu.Lock()
	defer startupCheckMu.Unlock()

	if startupCheckDone {
		return nil
	}
	if err := ensureRandPicIndexed(ctx); err != nil {
		return err
	}
	startupCheckDone = true
	return nil
}

func warmRandPicIndex() {
	if err := StartupCheck(context.Background()); err != nil {
		log.Warnf("[randpic] startup check failed: %v", err)
	}
}

func ensureRandPicIndexed(ctx context.Context) error {
	db := database.Get()
	if db == nil {
		return errors.New("database not initialized")
	}

	baseDir := randPicBaseDir()
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}

	if cfg := config.Get(); cfg != nil {
		for _, category := range cfg.RandPic.CommandList {
			category = normalizeCategory(category)
			if category == "" {
				continue
			}
			if err := os.MkdirAll(filepath.Join(baseDir, category), 0o755); err != nil {
				return err
			}
		}
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		category := normalizeCategory(entry.Name())
		if category == "" {
			continue
		}

		categoryDir := filepath.Join(baseDir, entry.Name())
		files, err := os.ReadDir(categoryDir)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir() || !isSupportedRandPicFilename(file.Name()) {
				continue
			}

			fullPath := filepath.Join(categoryDir, file.Name())
			hash, err := fileSHA256(fullPath)
			if err != nil {
				log.Warnf("[randpic] hash file %s failed: %v", fullPath, err)
				continue
			}

			item := &database.RandPicItem{
				Category: category,
				FileName: file.Name(),
				FilePath: filepath.ToSlash(fullPath),
				FileHash: hash,
			}
			if err := db.WithContext(ctx).
				Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "category"},
						{Name: "file_hash"},
					},
					DoUpdates: clause.AssignmentColumns([]string{"file_name", "file_path", "updated_at"}),
				}).
				Create(item).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func randPicBaseDir() string {
	if cfg := config.Get(); cfg != nil && strings.TrimSpace(cfg.RandPic.StoreDirPath) != "" {
		return cfg.RandPic.StoreDirPath
	}
	return dataDir
}

func isRandPicDisabledInCurrentChat(ctx *zero.Ctx) bool {
	if ctx == nil || ctx.Event == nil || ctx.Event.GroupID == 0 {
		return false
	}
	if cfg := config.Get(); cfg != nil {
		groupID := fmt.Sprintf("%d", ctx.Event.GroupID)
		for _, blocked := range cfg.RandPic.DisabledGroups {
			if strings.TrimSpace(blocked) == groupID {
				return true
			}
		}
	}
	return false
}

func isSupportedRandPicFilename(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "Zone.Identifier") {
		return false
	}

	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
