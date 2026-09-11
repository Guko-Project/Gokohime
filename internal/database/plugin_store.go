package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func dbOrGlobal(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return Get()
}

func RandomCPStory(ctx context.Context, tx *gorm.DB) (*CPStory, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var story CPStory
	if err := db.WithContext(ctx).Order("RANDOM()").Limit(1).Take(&story).Error; err != nil {
		return nil, err
	}
	return &story, nil
}

func MealExists(ctx context.Context, tx *gorm.DB, kind, meal string) (bool, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return false, errors.New("database not initialized")
	}

	var count int64
	if err := db.WithContext(ctx).
		Model(&MealEntry{}).
		Where("kind = ? AND name = ?", kind, strings.TrimSpace(meal)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func AddMealEntry(ctx context.Context, tx *gorm.DB, kind, meal, hash string) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	entry := &MealEntry{
		Kind:      kind,
		Name:      strings.TrimSpace(meal),
		EntryHash: hash,
	}
	return db.WithContext(ctx).Create(entry).Error
}

func DeleteMealEntry(ctx context.Context, tx *gorm.DB, kind, meal string) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	return db.WithContext(ctx).
		Where("kind = ? AND name = ?", kind, strings.TrimSpace(meal)).
		Delete(&MealEntry{}).Error
}

func RandomMealEntry(ctx context.Context, tx *gorm.DB, kind string) (*MealEntry, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var entry MealEntry
	if err := db.WithContext(ctx).
		Where("kind = ?", kind).
		Order("RANDOM()").
		Limit(1).
		Take(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func RandomKTVSong(ctx context.Context, tx *gorm.DB, category string) (*KTVSong, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	q := db.WithContext(ctx).Model(&KTVSong{})
	if category != "" && category != "0" {
		q = q.Where("category = ?", category)
	}

	var song KTVSong
	if err := q.Order("RANDOM()").Limit(1).Take(&song).Error; err != nil {
		return nil, err
	}
	return &song, nil
}

func KTVSongExists(ctx context.Context, tx *gorm.DB, name string) (bool, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return false, errors.New("database not initialized")
	}

	var count int64
	if err := db.WithContext(ctx).
		Model(&KTVSong{}).
		Where("name = ?", strings.TrimSpace(name)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func UpsertKTVSong(ctx context.Context, tx *gorm.DB, song *KTVSong) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	if song == nil {
		return errors.New("ktv song is nil")
	}
	song.Name = strings.TrimSpace(song.Name)
	song.Category = strings.TrimSpace(song.Category)
	song.BV = strings.TrimSpace(song.BV)
	song.Issuer = strings.TrimSpace(song.Issuer)
	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{"category", "bv", "issuer", "entry_hash", "updated_at"}),
		}).
		Create(song).Error
}

func DeleteKTVSong(ctx context.Context, tx *gorm.DB, name string) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	return db.WithContext(ctx).
		Where("name = ?", strings.TrimSpace(name)).
		Delete(&KTVSong{}).Error
}

func RandomKKSonglistSong(ctx context.Context, tx *gorm.DB, category string) (*KKSonglistSong, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	q := db.WithContext(ctx).Model(&KKSonglistSong{})
	if category = strings.TrimSpace(category); category != "" {
		q = q.Where("category = ?", category)
	}

	var song KKSonglistSong
	if err := q.Order("RANDOM()").Limit(1).Take(&song).Error; err != nil {
		return nil, err
	}
	return &song, nil
}

func KKSonglistCategoryOwner(ctx context.Context, tx *gorm.DB, category string) (string, bool, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return "", false, errors.New("database not initialized")
	}

	var song KKSonglistSong
	if err := db.WithContext(ctx).
		Where("category = ?", strings.TrimSpace(category)).
		Order("id ASC").
		Limit(1).
		Take(&song).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return song.Issuer, true, nil
}

func ReplaceKKSonglistCategory(ctx context.Context, tx *gorm.DB, category, issuer, sourceURL string, songs []string) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	category = strings.TrimSpace(category)
	issuer = strings.TrimSpace(issuer)
	sourceURL = strings.TrimSpace(sourceURL)
	if category == "" {
		return errors.New("kk songlist category is empty")
	}

	entries := make([]KKSonglistSong, 0, len(songs))
	for _, song := range songs {
		name := strings.TrimSpace(song)
		if name == "" {
			continue
		}
		entries = append(entries, KKSonglistSong{
			Category:  category,
			Name:      name,
			Issuer:    issuer,
			SourceURL: sourceURL,
		})
	}
	if len(entries) == 0 {
		return errors.New("kk songlist has no songs")
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("category = ?", category).Delete(&KKSonglistSong{}).Error; err != nil {
			return err
		}
		return tx.Create(&entries).Error
	})
}

func DeleteKKSonglistCategory(ctx context.Context, tx *gorm.DB, category string) (int64, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return 0, errors.New("database not initialized")
	}

	result := db.WithContext(ctx).
		Where("category = ?", strings.TrimSpace(category)).
		Delete(&KKSonglistSong{})
	return result.RowsAffected, result.Error
}

func DeleteKKSonglistSong(ctx context.Context, tx *gorm.DB, category, name string) (int64, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return 0, errors.New("database not initialized")
	}

	result := db.WithContext(ctx).
		Where("category = ? AND name = ?", strings.TrimSpace(category), strings.TrimSpace(name)).
		Delete(&KKSonglistSong{})
	return result.RowsAffected, result.Error
}

func ListKKSonglistCategoriesByIssuer(ctx context.Context, tx *gorm.DB, issuer string, limit int) ([]KKSonglistCategorySummary, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	if limit <= 0 {
		limit = 10
	}

	var summaries []KKSonglistCategorySummary
	if err := db.WithContext(ctx).
		Model(&KKSonglistSong{}).
		Select("category, COUNT(*) AS count").
		Where("issuer = ?", strings.TrimSpace(issuer)).
		Group("category").
		Order("MAX(updated_at) DESC").
		Limit(limit).
		Scan(&summaries).Error; err != nil {
		return nil, err
	}
	return summaries, nil
}

func DistinctRandPicCategories(ctx context.Context, tx *gorm.DB) ([]string, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var categories []string
	if err := db.WithContext(ctx).
		Model(&RandPicItem{}).
		Distinct().
		Order("category ASC").
		Pluck("category", &categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

func RandomRandPicItem(ctx context.Context, tx *gorm.DB, category string) (*RandPicItem, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var item RandPicItem
	if err := db.WithContext(ctx).
		Where("category = ?", category).
		Order("RANDOM()").
		Limit(1).
		Take(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func SearchRandPicItem(ctx context.Context, tx *gorm.DB, category, keyword string) (*RandPicItem, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return nil, gorm.ErrRecordNotFound
	}

	var assets []RandPicItem
	if err := db.WithContext(ctx).Where("category = ?", category).Find(&assets).Error; err != nil {
		return nil, err
	}

	var matches []RandPicItem
	for _, asset := range assets {
		var keywords []string
		if asset.KeywordsJSON != "" {
			_ = json.Unmarshal([]byte(asset.KeywordsJSON), &keywords)
		}
		for _, item := range keywords {
			if strings.Contains(strings.ToLower(item), needle) {
				matches = append(matches, asset)
				break
			}
		}
	}

	if len(matches) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	ids := make([]uint, len(matches))
	for i := range matches {
		ids[i] = matches[i].ID
	}

	var picked RandPicItem
	if err := db.WithContext(ctx).
		Where("id IN ?", ids).
		Order("RANDOM()").
		Take(&picked).Error; err == nil {
		return &picked, nil
	}
	return &matches[0], nil
}

func RandPicCategoryExists(ctx context.Context, tx *gorm.DB, category string) (bool, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return false, errors.New("database not initialized")
	}

	var count int64
	if err := db.WithContext(ctx).
		Model(&RandPicItem{}).
		Where("category = ?", strings.TrimSpace(category)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetDailyLuckTemplate(ctx context.Context, tx *gorm.DB, number int) (*DailyLuckTemplate, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var tmpl DailyLuckTemplate
	if err := db.WithContext(ctx).Where("number = ?", number).Take(&tmpl).Error; err != nil {
		return nil, err
	}
	return &tmpl, nil
}

func RandomGuessSongCatalog(ctx context.Context, tx *gorm.DB, library string) (*GuessSongCatalog, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var item GuessSongCatalog
	if err := db.WithContext(ctx).
		Where("library = ?", library).
		Order("RANDOM()").
		Limit(1).
		Take(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LoadGuessGameSession(ctx context.Context, tx *gorm.DB, groupID int64) (*GuessGameSession, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var session GuessGameSession
	if err := db.WithContext(ctx).Where("group_id = ?", groupID).Take(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func SaveGuessGameSession(ctx context.Context, tx *gorm.DB, session *GuessGameSession) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}
	if session == nil {
		return errors.New("session is nil")
	}
	if session.ID != 0 {
		return db.WithContext(ctx).Save(session).Error
	}
	values := map[string]any{
		"group_id":     session.GroupID,
		"library":      session.Library,
		"song_id":      session.SongID,
		"song_name":    session.SongName,
		"artist":       session.Artist,
		"audio_url":    session.AudioURL,
		"slice1_path":  session.Slice1Path,
		"slice2_path":  session.Slice2Path,
		"hint_stage":   session.HintStage,
		"answer_count": session.AnswerCount,
		"answer":       session.Answer,
		"prepared_at":  session.PreparedAt,
		"last_error":   session.LastError,
		"active":       session.Active,
		"created_at":   session.CreatedAt,
		"updated_at":   session.UpdatedAt,
	}
	if session.CreatedAt.IsZero() {
		values["created_at"] = time.Now()
	}
	if session.UpdatedAt.IsZero() {
		values["updated_at"] = time.Now()
	}
	return db.WithContext(ctx).
		Model(&GuessGameSession{}).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "group_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"library",
				"song_id",
				"song_name",
				"artist",
				"audio_url",
				"slice1_path",
				"slice2_path",
				"hint_stage",
				"answer_count",
				"answer",
				"prepared_at",
				"last_error",
				"active",
				"updated_at",
			}),
		}).
		Create(values).Error
}

func ClearGuessGameSession(ctx context.Context, tx *gorm.DB, groupID int64) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}
	return db.WithContext(ctx).
		Model(&GuessGameSession{}).
		Where("group_id = ?", groupID).
		Updates(map[string]any{
			"active":       false,
			"hint_stage":   0,
			"answer_count": 0,
		}).Error
}

func GetPluginKV(ctx context.Context, tx *gorm.DB, namespace, key string) (string, error) {
	db := dbOrGlobal(tx)
	if db == nil {
		return "", errors.New("database not initialized")
	}

	var kv PluginKV
	if err := db.WithContext(ctx).
		Where("namespace = ? AND key = ?", namespace, key).
		Take(&kv).Error; err != nil {
		return "", err
	}
	return kv.Value, nil
}

func SetPluginKV(ctx context.Context, tx *gorm.DB, namespace, key, value string) error {
	db := dbOrGlobal(tx)
	if db == nil {
		return errors.New("database not initialized")
	}

	kv := &PluginKV{
		Namespace: namespace,
		Key:       key,
		Value:     value,
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "namespace"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(kv).Error
}

func MustFormatHash(parts ...string) string {
	return strings.Join(parts, ":")
}

func ErrIfRecordNotFound(err error, fallback error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback
	}
	return err
}

func WrapNotFound(entity string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s not found", entity)
	}
	return err
}
