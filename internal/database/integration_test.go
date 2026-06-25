package database_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/testutil/dbtest"
	"gorm.io/gorm"
)

func TestPluginStoreIntegration(t *testing.T) {
	tx := dbtest.BeginTx(t)
	ctx := context.Background()
	dbtest.ResetModels(t, tx,
		&database.CPStory{},
		&database.MealEntry{},
		&database.KTVSong{},
		&database.RandPicItem{},
		&database.DailyLuckTemplate{},
		&database.GuessSongCatalog{},
		&database.PluginKV{},
	)

	storyHash := mustHash("cp", "story")
	if err := tx.Create(&database.CPStory{
		Story:     "<攻> 喜欢 <受>",
		StoryHash: storyHash,
	}).Error; err != nil {
		t.Fatalf("seed cp story failed: %v", err)
	}
	story, err := database.RandomCPStory(ctx, tx)
	if err != nil {
		t.Fatalf("RandomCPStory failed: %v", err)
	}
	if story.Story != "<攻> 喜欢 <受>" {
		t.Fatalf("unexpected story: %q", story.Story)
	}

	mealHash := mustHash("meal", "eatwhat", "烤肉")
	if err := database.AddMealEntry(ctx, tx, "eatwhat", "烤肉", mealHash); err != nil {
		t.Fatalf("AddMealEntry failed: %v", err)
	}
	exists, err := database.MealExists(ctx, tx, "eatwhat", "烤肉")
	if err != nil {
		t.Fatalf("MealExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected meal entry to exist")
	}
	meal, err := database.RandomMealEntry(ctx, tx, "eatwhat")
	if err != nil {
		t.Fatalf("RandomMealEntry failed: %v", err)
	}
	if meal.Name != "烤肉" {
		t.Fatalf("unexpected meal: %q", meal.Name)
	}
	if err := database.DeleteMealEntry(ctx, tx, "eatwhat", "烤肉"); err != nil {
		t.Fatalf("DeleteMealEntry failed: %v", err)
	}
	exists, err = database.MealExists(ctx, tx, "eatwhat", "烤肉")
	if err != nil {
		t.Fatalf("MealExists after delete failed: %v", err)
	}
	if exists {
		t.Fatalf("expected meal entry to be deleted")
	}

	if err := tx.Create(&database.KTVSong{
		Name:      "测试歌",
		Category:  "中",
		BV:        "BV1xx411c7mD",
		Issuer:    "tester",
		EntryHash: mustHash("ktv", "测试歌"),
	}).Error; err != nil {
		t.Fatalf("seed ktv song failed: %v", err)
	}
	song, err := database.RandomKTVSong(ctx, tx, "中")
	if err != nil {
		t.Fatalf("RandomKTVSong failed: %v", err)
	}
	if song.Name != "测试歌" {
		t.Fatalf("unexpected song: %q", song.Name)
	}
	if err := database.UpsertKTVSong(ctx, tx, &database.KTVSong{
		Name:      "追加歌",
		Category:  "日",
		BV:        "BV2xx411c7mD",
		Issuer:    "tester",
		EntryHash: mustHash("ktv", "追加歌"),
	}); err != nil {
		t.Fatalf("UpsertKTVSong failed: %v", err)
	}
	exists, err = database.KTVSongExists(ctx, tx, "追加歌")
	if err != nil {
		t.Fatalf("KTVSongExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected ktv song to exist")
	}
	if err := database.UpsertKTVSong(ctx, tx, &database.KTVSong{
		Name:      "追加歌",
		Category:  "中",
		BV:        "BV3xx411c7mD",
		Issuer:    "updater",
		EntryHash: mustHash("ktv", "追加歌"),
	}); err != nil {
		t.Fatalf("UpsertKTVSong update failed: %v", err)
	}
	updated, err := database.RandomKTVSong(ctx, tx, "中")
	if err != nil {
		t.Fatalf("RandomKTVSong after update failed: %v", err)
	}
	if updated.Name != "追加歌" || updated.BV != "BV3xx411c7mD" || updated.Issuer != "updater" {
		t.Fatalf("unexpected updated ktv song: %+v", updated)
	}
	if err := database.DeleteKTVSong(ctx, tx, "追加歌"); err != nil {
		t.Fatalf("DeleteKTVSong failed: %v", err)
	}
	exists, err = database.KTVSongExists(ctx, tx, "追加歌")
	if err != nil {
		t.Fatalf("KTVSongExists after delete failed: %v", err)
	}
	if exists {
		t.Fatalf("expected ktv song to be deleted")
	}

	keywords, _ := json.Marshal([]string{"早上好", "问候"})
	if err := tx.Create(&database.RandPicItem{
		Category:     "fu",
		FileName:     "fu.png",
		FilePath:     "data/randpic/fu/fu.png",
		FileHash:     mustHash("randpic", "fu"),
		KeywordsJSON: string(keywords),
	}).Error; err != nil {
		t.Fatalf("seed randpic item failed: %v", err)
	}
	if err := tx.Create(&database.RandPicItem{
		Category: "dly",
		FileName: "dly.png",
		FilePath: "data/randpic/dly/dly.png",
		FileHash: mustHash("randpic", "dly"),
	}).Error; err != nil {
		t.Fatalf("seed randpic item failed: %v", err)
	}
	categories, err := database.DistinctRandPicCategories(ctx, tx)
	if err != nil {
		t.Fatalf("DistinctRandPicCategories failed: %v", err)
	}
	sort.Strings(categories)
	if len(categories) != 2 || categories[0] != "dly" || categories[1] != "fu" {
		t.Fatalf("unexpected categories: %#v", categories)
	}
	item, err := database.RandomRandPicItem(ctx, tx, "fu")
	if err != nil {
		t.Fatalf("RandomRandPicItem failed: %v", err)
	}
	if item.FilePath != "data/randpic/fu/fu.png" {
		t.Fatalf("unexpected randpic item path: %q", item.FilePath)
	}
	item, err = database.SearchRandPicItem(ctx, tx, "fu", "问候")
	if err != nil {
		t.Fatalf("SearchRandPicItem failed: %v", err)
	}
	if item.Category != "fu" {
		t.Fatalf("unexpected search result category: %q", item.Category)
	}
	ok, err := database.RandPicCategoryExists(ctx, tx, "fu")
	if err != nil {
		t.Fatalf("RandPicCategoryExists failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected randpic category to exist")
	}

	if err := tx.Create(&database.DailyLuckTemplate{
		Number:   7,
		Text:     "签 吉\n万事顺利",
		ImageURL: "https://example.com/7.png",
	}).Error; err != nil {
		t.Fatalf("seed daily luck template failed: %v", err)
	}
	tmpl, err := database.GetDailyLuckTemplate(ctx, tx, 7)
	if err != nil {
		t.Fatalf("GetDailyLuckTemplate failed: %v", err)
	}
	if tmpl.ImageURL != "https://example.com/7.png" {
		t.Fatalf("unexpected template image url: %q", tmpl.ImageURL)
	}

	if err := tx.Create(&database.GuessSongCatalog{
		Library:  "rv",
		SongID:   42,
		MetaJSON: `{"id":42,"name":"测试歌"}`,
	}).Error; err != nil {
		t.Fatalf("seed guess song catalog failed: %v", err)
	}
	catalog, err := database.RandomGuessSongCatalog(ctx, tx, "rv")
	if err != nil {
		t.Fatalf("RandomGuessSongCatalog failed: %v", err)
	}
	if catalog.SongID != 42 {
		t.Fatalf("unexpected catalog song id: %d", catalog.SongID)
	}

	if err := database.SetPluginKV(ctx, tx, "test", "key", "value"); err != nil {
		t.Fatalf("SetPluginKV failed: %v", err)
	}
	value, err := database.GetPluginKV(ctx, tx, "test", "key")
	if err != nil {
		t.Fatalf("GetPluginKV failed: %v", err)
	}
	if value != "value" {
		t.Fatalf("unexpected plugin kv value: %q", value)
	}
}

func TestGuessGameSessionUpsertIntegration(t *testing.T) {
	tx := dbtest.BeginTx(t)
	ctx := context.Background()
	dbtest.ResetModels(t, tx, &database.GuessGameSession{})

	first := &database.GuessGameSession{
		GroupID:     13579,
		Library:     "rv",
		SongID:      1,
		SongName:    "第一首",
		Artist:      "歌手甲",
		AudioURL:    "https://example.com/1.mp3",
		Slice1Path:  "data/guesssong/group_13579/slice_1.wav",
		Slice2Path:  "data/guesssong/group_13579/slice_2.wav",
		HintStage:   0,
		AnswerCount: 1,
		Answer:      "第一首",
		PreparedAt:  time.Now().Add(-time.Minute),
		Active:      true,
	}
	if err := database.SaveGuessGameSession(ctx, tx, first); err != nil {
		t.Fatalf("SaveGuessGameSession initial failed: %v", err)
	}

	second := &database.GuessGameSession{
		GroupID:     13579,
		Library:     "cv",
		SongID:      2,
		SongName:    "第二首",
		Artist:      "歌手乙",
		AudioURL:    "https://example.com/2.mp3",
		Slice1Path:  "data/guesssong/group_13579/slice_1.wav",
		Slice2Path:  "data/guesssong/group_13579/slice_2.wav",
		HintStage:   2,
		AnswerCount: 4,
		Answer:      "第二首",
		PreparedAt:  time.Now(),
		LastError:   "none",
		Active:      false,
	}
	if err := database.SaveGuessGameSession(ctx, tx, second); err != nil {
		t.Fatalf("SaveGuessGameSession upsert failed: %v", err)
	}

	var count int64
	if err := tx.Model(&database.GuessGameSession{}).Where("group_id = ?", 13579).Count(&count).Error; err != nil {
		t.Fatalf("count guess sessions failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one guess session row after upsert, got %d", count)
	}

	session, err := database.LoadGuessGameSession(ctx, tx, 13579)
	if err != nil {
		t.Fatalf("LoadGuessGameSession failed: %v", err)
	}
	if session.Library != "cv" || session.SongID != 2 || session.AnswerCount != 4 || session.Active {
		t.Fatalf("unexpected session after upsert: %+v", session)
	}

	if err := database.ClearGuessGameSession(ctx, tx, 13579); err != nil {
		t.Fatalf("ClearGuessGameSession failed: %v", err)
	}
	session, err = database.LoadGuessGameSession(ctx, tx, 13579)
	if err != nil {
		t.Fatalf("LoadGuessGameSession after clear failed: %v", err)
	}
	if session.Active || session.HintStage != 0 || session.AnswerCount != 0 {
		t.Fatalf("unexpected session after clear: %+v", session)
	}
}

func TestErrHelpers(t *testing.T) {
	fallback := errors.New("fallback")
	if got := database.ErrIfRecordNotFound(gorm.ErrRecordNotFound, fallback); !errors.Is(got, fallback) {
		t.Fatalf("ErrIfRecordNotFound did not return fallback: %v", got)
	}
	if got := database.WrapNotFound("meal", gorm.ErrRecordNotFound); got == nil || got.Error() != "meal not found" {
		t.Fatalf("WrapNotFound returned unexpected error: %v", got)
	}
}

func mustHash(parts ...string) string {
	sum := sha256.Sum256([]byte(stringsJoin(parts)))
	return hex.EncodeToString(sum[:])
}

func stringsJoin(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += "\n" + part
	}
	return out
}
