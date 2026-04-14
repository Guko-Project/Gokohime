package migrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Mode string

const (
	ModeAll            Mode = "all"
	ModeArchiveOnly    Mode = "archive-only"
	ModeStructuredOnly Mode = "structured-only"
)

type Options struct {
	Mode Mode
}

type Stats struct {
	JSONFilesArchived   int      `json:"json_files_archived"`
	BinaryFilesArchived int      `json:"binary_files_archived"`
	CPStories           int      `json:"cp_stories"`
	MealEntries         int      `json:"meal_entries"`
	KTVSongs            int      `json:"ktv_songs"`
	LuckTemplates       int      `json:"luck_templates"`
	GuessSongCatalog    int      `json:"guess_song_catalog"`
	RandPicItems        int      `json:"randpic_items"`
	SkippedFiles        int      `json:"skipped_files"`
	Errors              []string `json:"errors,omitempty"`
}

type ktvSongSource struct {
	BV       *string `json:"bv"`
	Category *string `json:"catagory"`
	Issuer   string  `json:"issuer"`
}

type structuredSource struct {
	CanonicalPath string
	FallbackPaths []string
	Import        func(context.Context, *gorm.DB, string, *Stats) error
}

var structuredSources = []structuredSource{
	{CanonicalPath: "data/cp/cp_story.json", Import: importCPStories},
	{
		CanonicalPath: "data/eatwhat/meal.json",
		FallbackPaths: []string{"ref/src/plugins/eatwhat/meal.json"},
		Import:        importMealEntries,
	},
	{CanonicalPath: "data/kk/ktv.json", Import: importKTVSongs},
	{
		CanonicalPath: "data/ref/guess_song/cv.json",
		FallbackPaths: []string{"ref/src/plugins/guess_song/cv.json"},
		Import:        importGuessSongs,
	},
	{
		CanonicalPath: "data/ref/guess_song/rv.json",
		FallbackPaths: []string{"ref/src/plugins/guess_song/rv.json"},
		Import:        importGuessSongs,
	},
	{
		CanonicalPath: "data/ref/jrluck/luck_data.json",
		FallbackPaths: []string{"ref/src/plugins/jrluck/luck_data.json"},
		Import:        importDailyLuckTemplates,
	},
}

func Run(ctx context.Context, db *gorm.DB, repoRoot string) (*Stats, error) {
	return RunWithOptions(ctx, db, repoRoot, Options{Mode: ModeAll})
}

func RunWithOptions(ctx context.Context, db *gorm.DB, repoRoot string, options Options) (*Stats, error) {
	mode := normalizeMode(options.Mode)
	stats := &Stats{}
	run := &database.MigrationRun{
		Name:   fmt.Sprintf("data-import:%s", mode),
		Status: "running",
	}
	if err := db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, fmt.Errorf("create migration run: %w", err)
	}

	finish := func(status string, err error) (*Stats, error) {
		statsJSON, _ := json.Marshal(stats)
		summary := fmt.Sprintf(
			"mode=%s structured=%d json=%d binary=%d skipped=%d errors=%d",
			mode,
			stats.StructuredRecords(),
			stats.JSONFilesArchived,
			stats.BinaryFilesArchived,
			stats.SkippedFiles,
			len(stats.Errors),
		)
		updates := map[string]any{
			"status":     status,
			"summary":    summary,
			"stats_json": string(statsJSON),
			"updated_at": time.Now(),
		}
		_ = db.WithContext(ctx).Model(run).Updates(updates).Error
		if err != nil {
			return stats, err
		}
		return stats, nil
	}

	dataRoot := filepath.Join(repoRoot, "data")
	if mode.archiveEnabled() {
		if err := archiveJSONFiles(ctx, db, repoRoot, dataRoot, stats); err != nil {
			return finish("failed", err)
		}
		if err := archiveBinaryFiles(ctx, db, repoRoot, dataRoot, stats); err != nil {
			return finish("failed", err)
		}
	}
	if mode.structuredEnabled() {
		if err := importStructuredSources(ctx, db, repoRoot, stats); err != nil {
			return finish("failed", err)
		}
		if err := importRandPicAssets(ctx, db, repoRoot, stats); err != nil {
			return finish("failed", err)
		}
		if err := importSayingAssets(ctx, db, repoRoot, stats); err != nil {
			return finish("failed", err)
		}
	}

	return finish("completed", nil)
}

func (s *Stats) StructuredRecords() int {
	if s == nil {
		return 0
	}
	return s.CPStories + s.MealEntries + s.KTVSongs + s.LuckTemplates + s.GuessSongCatalog + s.RandPicItems
}

func normalizeMode(mode Mode) Mode {
	switch strings.TrimSpace(strings.ToLower(string(mode))) {
	case string(ModeArchiveOnly):
		return ModeArchiveOnly
	case string(ModeStructuredOnly):
		return ModeStructuredOnly
	default:
		return ModeAll
	}
}

func (m Mode) archiveEnabled() bool {
	return m == ModeAll || m == ModeArchiveOnly
}

func (m Mode) structuredEnabled() bool {
	return m == ModeAll || m == ModeStructuredOnly
}

func archiveJSONFiles(ctx context.Context, db *gorm.DB, repoRoot, dataRoot string, stats *Stats) error {
	seen := make(map[string]struct{})

	if err := filepath.WalkDir(dataRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if shouldIgnoreArchivedFile(rel) {
			stats.SkippedFiles++
			return nil
		}
		if !isJSONFile(path) {
			return nil
		}

		if err := archiveJSONFile(ctx, db, path, rel); err != nil {
			return err
		}
		stats.JSONFilesArchived++
		seen[rel] = struct{}{}
		return nil
	}); err != nil {
		return err
	}

	for _, source := range structuredSources {
		if _, ok := seen[source.CanonicalPath]; ok {
			continue
		}

		path, ok := resolveSourcePath(repoRoot, source.CanonicalPath, source.FallbackPaths)
		if !ok {
			continue
		}
		if err := archiveJSONFile(ctx, db, path, source.CanonicalPath); err != nil {
			return err
		}
		stats.JSONFilesArchived++
	}

	return nil
}

func archiveBinaryFiles(ctx context.Context, db *gorm.DB, repoRoot, dataRoot string, stats *Stats) error {
	return filepath.WalkDir(dataRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case shouldIgnoreArchivedFile(rel):
			stats.SkippedFiles++
			return nil
		case isJSONFile(path):
			return nil
		case shouldSkipRuntimeGeneratedBinary(rel):
			stats.SkippedFiles++
			return nil
		case shouldHandleInRandPicTable(rel):
			stats.SkippedFiles++
			return nil
		}

		if err := archiveBinaryFile(ctx, db, path, rel); err != nil {
			return err
		}
		stats.BinaryFilesArchived++
		return nil
	})
}

func archiveJSONFile(ctx context.Context, db *gorm.DB, path, sourcePath string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(raw)
	record := &database.PluginDataFile{
		SourcePath:   filepath.ToSlash(sourcePath),
		DataCategory: categorizePath(filepath.ToSlash(sourcePath)),
		SHA256:       hex.EncodeToString(sum[:]),
		FileSize:     int64(len(raw)),
		MimeType:     "application/json",
		RawJSON:      string(raw),
	}

	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_path"}},
			DoUpdates: clause.AssignmentColumns([]string{"data_category", "sha256", "file_size", "mime_type", "raw_json", "updated_at"}),
		}).
		Create(record).Error
}

func archiveBinaryFile(ctx context.Context, db *gorm.DB, path, sourcePath string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(raw)
	record := &database.PluginBinaryFile{
		SourcePath:   filepath.ToSlash(sourcePath),
		DataCategory: categorizePath(filepath.ToSlash(sourcePath)),
		SHA256:       hex.EncodeToString(sum[:]),
		FileSize:     int64(len(raw)),
		MimeType:     detectMimeType(path, raw),
		RawContent:   raw,
	}

	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_path"}},
			DoUpdates: clause.AssignmentColumns([]string{"data_category", "sha256", "file_size", "mime_type", "raw_content", "updated_at"}),
		}).
		Create(record).Error
}

func importStructuredSources(ctx context.Context, db *gorm.DB, repoRoot string, stats *Stats) error {
	for _, source := range structuredSources {
		path, ok := resolveSourcePath(repoRoot, source.CanonicalPath, source.FallbackPaths)
		if !ok {
			continue
		}
		if err := source.Import(ctx, db, path, stats); err != nil {
			return fmt.Errorf("import %s: %w", source.CanonicalPath, err)
		}
	}
	return nil
}

func importCPStories(ctx context.Context, db *gorm.DB, path string, stats *Stats) error {
	type payload struct {
		Records []struct {
			Story string `json:"story"`
		} `json:"RECORDS"`
	}

	var data payload
	if err := readJSON(path, &data); err != nil {
		return err
	}

	for _, record := range data.Records {
		story := strings.TrimSpace(record.Story)
		if story == "" {
			continue
		}
		hash := hashStrings("cp_story", story)
		entry := &database.CPStory{Story: story, StoryHash: hash}
		if err := db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "story_hash"}},
				DoUpdates: clause.AssignmentColumns([]string{"story", "updated_at"}),
			}).
			Create(entry).Error; err != nil {
			return err
		}
		stats.CPStories++
	}
	return nil
}

func importMealEntries(ctx context.Context, db *gorm.DB, path string, stats *Stats) error {
	var data map[string][]string
	if err := readJSON(path, &data); err != nil {
		return err
	}

	kinds := make([]string, 0, len(data))
	for kind := range data {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		for _, item := range data[kind] {
			name := strings.TrimSpace(item)
			if name == "" {
				continue
			}
			hash := hashStrings("meal", kind, name)
			entry := &database.MealEntry{
				Kind:      kind,
				Name:      name,
				EntryHash: hash,
			}
			if err := db.WithContext(ctx).
				Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "entry_hash"}},
					DoUpdates: clause.AssignmentColumns([]string{"kind", "name", "updated_at"}),
				}).
				Create(entry).Error; err != nil {
				return err
			}
			stats.MealEntries++
		}
	}
	return nil
}

func importKTVSongs(ctx context.Context, db *gorm.DB, path string, stats *Stats) error {
	var data map[string]ktvSongSource
	if err := readJSON(path, &data); err != nil {
		return err
	}

	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		item := data[name]
		category := ""
		if item.Category != nil {
			category = *item.Category
		}
		bv := ""
		if item.BV != nil {
			bv = *item.BV
		}
		entry := &database.KTVSong{
			Name:      name,
			Category:  category,
			BV:        bv,
			Issuer:    item.Issuer,
			EntryHash: hashStrings("ktv", name),
		}
		if err := db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "entry_hash"}},
				DoUpdates: clause.AssignmentColumns([]string{"name", "category", "bv", "issuer", "updated_at"}),
			}).
			Create(entry).Error; err != nil {
			return err
		}
		stats.KTVSongs++
	}
	return nil
}

func importGuessSongs(ctx context.Context, db *gorm.DB, path string, stats *Stats) error {
	var data []map[string]any
	if err := readJSON(path, &data); err != nil {
		return err
	}

	library := "rv"
	if strings.Contains(strings.ToLower(filepath.Base(path)), "cv") {
		library = "cv"
	}

	for _, item := range data {
		value, ok := item["id"]
		if !ok {
			continue
		}
		songID, ok := asInt64(value)
		if !ok {
			continue
		}
		meta, _ := json.Marshal(item)
		entry := &database.GuessSongCatalog{
			Library:  library,
			SongID:   songID,
			MetaJSON: string(meta),
		}
		if err := db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "library"}, {Name: "song_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"meta_json", "updated_at"}),
			}).
			Create(entry).Error; err != nil {
			return err
		}
		stats.GuessSongCatalog++
	}
	return nil
}

func importDailyLuckTemplates(ctx context.Context, db *gorm.DB, path string, stats *Stats) error {
	var data []struct {
		Fields struct {
			Number int    `json:"number"`
			Text   string `json:"text"`
			ImgURL string `json:"img_url"`
		} `json:"fields"`
	}
	if err := readJSON(path, &data); err != nil {
		return err
	}

	for _, item := range data {
		entry := &database.DailyLuckTemplate{
			Number:   item.Fields.Number,
			Text:     item.Fields.Text,
			ImageURL: item.Fields.ImgURL,
		}
		if err := db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "number"}},
				DoUpdates: clause.AssignmentColumns([]string{"text", "image_url", "updated_at"}),
			}).
			Create(entry).Error; err != nil {
			return err
		}
		stats.LuckTemplates++
	}
	return nil
}

func importRandPicAssets(ctx context.Context, db *gorm.DB, repoRoot string, stats *Stats) error {
	root := filepath.Join(repoRoot, "data", "randpic")
	if _, err := os.Stat(root); err != nil {
		return nil
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isSupportedImageFile(d.Name()) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return nil
		}

		hash, err := fileHash(path)
		if err != nil {
			return err
		}
		repoRel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		entry := &database.RandPicItem{
			Category: parts[0],
			FileName: filepath.Base(path),
			FilePath: filepath.ToSlash(repoRel),
			FileHash: hash,
		}
		if err := upsertRandPicItem(ctx, db, entry); err != nil {
			return err
		}
		stats.RandPicItems++
		return nil
	})
}

func importSayingAssets(ctx context.Context, db *gorm.DB, repoRoot string, stats *Stats) error {
	root := filepath.Join(repoRoot, "data", "sayings")
	if _, err := os.Stat(root); err != nil {
		return nil
	}

	manifest := loadLegacySayingManifest(repoRoot)
	keywordIndex := loadLegacyKeywordIndex(root)

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		repoRel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		repoRel = filepath.ToSlash(repoRel)

		if shouldIgnoreArchivedFile(repoRel) {
			return nil
		}
		if !isSupportedImageFile(d.Name()) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return nil
		}
		category := strings.ToLower(strings.TrimSpace(parts[0]))
		if !isLegacySayingCategory(category) {
			return nil
		}

		hash, err := fileHash(path)
		if err != nil {
			return err
		}

		canonicalPath := canonicalRandPicPath(repoRoot, repoRel, category, filepath.Base(path), manifest)
		keywordsJSON := ""
		if category == "fu" {
			keywords, _ := json.Marshal(keywordIndex[filepath.Base(path)])
			keywordsJSON = string(keywords)
		}

		entry := &database.RandPicItem{
			Category:     category,
			FileName:     filepath.Base(canonicalPath),
			FilePath:     canonicalPath,
			FileHash:     hash,
			KeywordsJSON: keywordsJSON,
		}
		if err := upsertRandPicItem(ctx, db, entry); err != nil {
			return err
		}
		stats.RandPicItems++
		return nil
	})
}

func loadLegacyKeywordIndex(root string) map[string][]string {
	keywordIndex := map[string][]string{}
	for _, name := range []string{"inverted_index.json", "inverted_index-nonum.json"} {
		path := filepath.Join(root, name)
		data := map[string][]string{}
		if err := readJSON(path, &data); err != nil {
			continue
		}
		for keyword, files := range data {
			for _, fileName := range files {
				fileName = strings.TrimSpace(fileName)
				if fileName == "" {
					continue
				}
				keywordIndex[fileName] = append(keywordIndex[fileName], keyword)
			}
		}
	}

	for fileName, keywords := range keywordIndex {
		keywordIndex[fileName] = uniqStrings(keywords)
	}
	return keywordIndex
}

func canonicalRandPicPath(repoRoot, sourcePath, category, fileName string, manifest map[string]string) string {
	if mapped, ok := manifest[sourcePath]; ok && strings.TrimSpace(mapped) != "" {
		return filepath.ToSlash(mapped)
	}

	canonical := filepath.ToSlash(filepath.Join("data", "randpic", category, fileName))
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(canonical))); err == nil {
		return canonical
	}
	return canonical
}

func loadLegacySayingManifest(repoRoot string) map[string]string {
	manifestPath := filepath.Join(repoRoot, "data", "randpic", "_legacy_sayings_manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return map[string]string{}
	}
	manifest := map[string]string{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return map[string]string{}
	}
	return manifest
}

func upsertRandPicItem(ctx context.Context, db *gorm.DB, entry *database.RandPicItem) error {
	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "category"},
				{Name: "file_hash"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"file_name", "file_path", "keywords_json", "updated_at"}),
		}).
		Create(entry).Error
}

func resolveSourcePath(repoRoot, canonicalPath string, fallbackPaths []string) (string, bool) {
	candidates := append([]string{canonicalPath}, fallbackPaths...)
	for _, rel := range candidates {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err == nil && !info.IsDir() {
			return abs, true
		}
	}
	return "", false
}

func shouldIgnoreArchivedFile(path string) bool {
	path = filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	if path == "" {
		return true
	}
	base := filepath.Base(path)
	switch {
	case strings.Contains(path, "zone.identifier"):
		return true
	case strings.HasSuffix(base, ".db-journal"), strings.HasSuffix(base, ".journal"):
		return true
	case strings.HasSuffix(base, ".tmp"), strings.HasSuffix(base, ".temp"), strings.HasSuffix(base, ".swp"), strings.HasSuffix(base, ".bak"), strings.HasSuffix(base, "~"):
		return true
	case strings.HasSuffix(base, ".tgz"), strings.HasSuffix(base, ".tar"), strings.HasSuffix(base, ".gz"), strings.HasSuffix(base, ".zip"), strings.HasSuffix(base, ".rar"), strings.HasSuffix(base, ".7z"):
		return true
	case base == ".ds_store", base == "thumbs.db":
		return true
	case strings.HasPrefix(path, "data/sayings/fuocr/"), strings.HasPrefix(path, "data/sayings/memedb/"), strings.HasPrefix(path, "data/sayings/ocr/"):
		return true
	default:
		return false
	}
}

func shouldSkipRuntimeGeneratedBinary(path string) bool {
	path = filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	return strings.HasPrefix(path, "data/guesssong/") || strings.Contains(path, "/guesssong/")
}

func shouldHandleInRandPicTable(path string) bool {
	path = filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	return strings.HasPrefix(path, "data/randpic/") ||
		strings.HasPrefix(path, "data/sayings/") ||
		strings.Contains(path, "/randpic/") ||
		strings.Contains(path, "/sayings/")
}

func isJSONFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".json")
}

func isSupportedImageFile(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.Contains(name, "zone.identifier") {
		return false
	}
	return strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".gif") ||
		strings.HasSuffix(name, ".webp")
}

func isLegacySayingCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "fu", "gst", "gu", "kira", "motohg", "pjsk", "rui", "wt", "tls":
		return true
	default:
		return false
	}
}

func detectMimeType(path string, data []byte) string {
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); contentType != "" {
		return contentType
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}

func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse json %s: %w", path, err)
	}
	return nil
}

func hashStrings(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func fileHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	case json.Number:
		i, err := t.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func categorizePath(path string) string {
	switch {
	case path == "data/help.png":
		return "help"
	case strings.Contains(path, "/cp/"):
		return "cp"
	case strings.Contains(path, "/eatwhat/"):
		return "eatwhat"
	case strings.Contains(path, "/kk/"):
		return "kk"
	case strings.Contains(path, "/guess_song/"):
		return "guess_song"
	case strings.Contains(path, "/jrluck/"):
		return "jrluck"
	case strings.Contains(path, "/jrrp/"):
		return "jrrp"
	case strings.Contains(path, "/guesssong/"):
		return "guesssong"
	case strings.Contains(path, "/randpic/"):
		return "randpic"
	case strings.Contains(path, "/sayings/"):
		return "sayings"
	case strings.Contains(path, "/maimaidx/"):
		return "maimaidx"
	case strings.Contains(path, "/stickers/"):
		return "stickers"
	case strings.Contains(path, "/ref/"):
		return "ref"
	default:
		return "generic"
	}
}

func uniqStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}
