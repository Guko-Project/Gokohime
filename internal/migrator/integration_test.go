package migrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/testutil/dbtest"
	"gorm.io/gorm"
)

func TestRunWithOptionsAllIntegration(t *testing.T) {
	tx := dbtest.BeginTx(t)
	dbtest.ResetModels(t, tx,
		&database.PluginDataFile{},
		&database.PluginBinaryFile{},
		&database.CPStory{},
		&database.MealEntry{},
		&database.KTVSong{},
		&database.GuessSongCatalog{},
		&database.DailyLuckTemplate{},
		&database.RandPicItem{},
		&database.MigrationRun{},
	)
	root := seedMigrationFixture(t)

	stats, err := RunWithOptions(context.Background(), tx, root, Options{Mode: ModeAll})
	if err != nil {
		t.Fatalf("RunWithOptions(all) failed: %v", err)
	}
	assertStats(t, stats, 10, 4, 1, 2, 1, 1, 2, 3)

	assertTableCount(t, tx, &database.PluginDataFile{}, 10)
	assertTableCount(t, tx, &database.PluginBinaryFile{}, 4)
	assertTableCount(t, tx, &database.CPStory{}, 1)
	assertTableCount(t, tx, &database.MealEntry{}, 2)
	assertTableCount(t, tx, &database.KTVSong{}, 1)
	assertTableCount(t, tx, &database.GuessSongCatalog{}, 2)
	assertTableCount(t, tx, &database.DailyLuckTemplate{}, 1)
	assertTableCount(t, tx, &database.RandPicItem{}, 2)
	assertTableCount(t, tx, &database.MigrationRun{}, 1)

	var fu database.RandPicItem
	if err := tx.Where("category = ?", "fu").Take(&fu).Error; err != nil {
		t.Fatalf("load fu randpic item failed: %v", err)
	}
	if fu.FilePath != "data/randpic/fu/legacy.png" {
		t.Fatalf("unexpected fu file path: %q", fu.FilePath)
	}
	if !strings.Contains(fu.KeywordsJSON, "早安") || !strings.Contains(fu.KeywordsJSON, "问候") {
		t.Fatalf("unexpected fu keywords json: %q", fu.KeywordsJSON)
	}

	var dly database.RandPicItem
	if err := tx.Where("category = ?", "dly").Take(&dly).Error; err != nil {
		t.Fatalf("load dly randpic item failed: %v", err)
	}
	if dly.KeywordsJSON != "" {
		t.Fatalf("expected non-fu category to keep empty keywords, got %q", dly.KeywordsJSON)
	}

	stats, err = RunWithOptions(context.Background(), tx, root, Options{Mode: ModeAll})
	if err != nil {
		t.Fatalf("RunWithOptions(all) second pass failed: %v", err)
	}
	assertStats(t, stats, 10, 4, 1, 2, 1, 1, 2, 3)

	assertTableCount(t, tx, &database.PluginDataFile{}, 10)
	assertTableCount(t, tx, &database.PluginBinaryFile{}, 4)
	assertTableCount(t, tx, &database.CPStory{}, 1)
	assertTableCount(t, tx, &database.MealEntry{}, 2)
	assertTableCount(t, tx, &database.KTVSong{}, 1)
	assertTableCount(t, tx, &database.GuessSongCatalog{}, 2)
	assertTableCount(t, tx, &database.DailyLuckTemplate{}, 1)
	assertTableCount(t, tx, &database.RandPicItem{}, 2)
	assertTableCount(t, tx, &database.MigrationRun{}, 2)
}

func TestRunWithOptionsModeSelection(t *testing.T) {
	t.Run("archive-only", func(t *testing.T) {
		tx := dbtest.BeginTx(t)
		dbtest.ResetModels(t, tx,
			&database.PluginDataFile{},
			&database.PluginBinaryFile{},
			&database.CPStory{},
			&database.MealEntry{},
			&database.KTVSong{},
			&database.GuessSongCatalog{},
			&database.DailyLuckTemplate{},
			&database.RandPicItem{},
			&database.MigrationRun{},
		)
		root := seedMigrationFixture(t)

		stats, err := RunWithOptions(context.Background(), tx, root, Options{Mode: ModeArchiveOnly})
		if err != nil {
			t.Fatalf("RunWithOptions(archive-only) failed: %v", err)
		}
		if stats.JSONFilesArchived == 0 || stats.BinaryFilesArchived == 0 {
			t.Fatalf("expected archive-only mode to archive files, got %+v", stats)
		}
		if stats.StructuredRecords() != 0 {
			t.Fatalf("expected archive-only mode to skip structured import, got %+v", stats)
		}

		assertTableCount(t, tx, &database.PluginDataFile{}, 10)
		assertTableCount(t, tx, &database.PluginBinaryFile{}, 4)
		assertTableCount(t, tx, &database.CPStory{}, 0)
		assertTableCount(t, tx, &database.RandPicItem{}, 0)
	})

	t.Run("structured-only", func(t *testing.T) {
		tx := dbtest.BeginTx(t)
		dbtest.ResetModels(t, tx,
			&database.PluginDataFile{},
			&database.PluginBinaryFile{},
			&database.CPStory{},
			&database.MealEntry{},
			&database.KTVSong{},
			&database.GuessSongCatalog{},
			&database.DailyLuckTemplate{},
			&database.RandPicItem{},
			&database.MigrationRun{},
		)
		root := seedMigrationFixture(t)

		stats, err := RunWithOptions(context.Background(), tx, root, Options{Mode: ModeStructuredOnly})
		if err != nil {
			t.Fatalf("RunWithOptions(structured-only) failed: %v", err)
		}
		if stats.JSONFilesArchived != 0 || stats.BinaryFilesArchived != 0 {
			t.Fatalf("expected structured-only mode to skip archive work, got %+v", stats)
		}
		if stats.StructuredRecords() == 0 {
			t.Fatalf("expected structured-only mode to import structured data, got %+v", stats)
		}

		assertTableCount(t, tx, &database.PluginDataFile{}, 0)
		assertTableCount(t, tx, &database.PluginBinaryFile{}, 0)
		assertTableCount(t, tx, &database.CPStory{}, 1)
		assertTableCount(t, tx, &database.RandPicItem{}, 2)
	})
}

func seedMigrationFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	writeFixtureFile(t, root, "data/cp/cp_story.json", `{"RECORDS":[{"story":"<攻> 喜欢 <受>"}]}`)
	writeFixtureFile(t, root, "data/eatwhat/meal.json", `{"eatwhat":["烤肉","寿司"]}`)
	writeFixtureFile(t, root, "data/kk/ktv.json", `{"测试歌":{"bv":"BV1xx411c7mD","catagory":"中","issuer":"tester"}}`)
	writeFixtureFile(t, root, "data/randpic/_legacy_sayings_manifest.json", `{"data/sayings/fu/legacy.png":"data/randpic/fu/legacy.png"}`)
	writeFixtureFile(t, root, "data/sayings/inverted_index.json", `{"早安":["legacy.png"]}`)
	writeFixtureFile(t, root, "data/sayings/inverted_index-nonum.json", `{"问候":["legacy.png"]}`)
	writeFixtureFile(t, root, "data/maimaidx/static.old/config.json", `{"enabled":true}`)

	writeFixtureFile(t, root, "ref/src/plugins/guess_song/cv.json", `[{"id":1001,"name":"CV曲"}]`)
	writeFixtureFile(t, root, "ref/src/plugins/guess_song/rv.json", `[{"id":2002,"name":"RV曲"}]`)
	writeFixtureFile(t, root, "ref/src/plugins/jrluck/luck_data.json", `[{"fields":{"number":1,"text":"签 吉\n很好","img_url":"https://example.com/1.png"}}]`)

	writeFixtureFile(t, root, "data/help.png", "help-png")
	writeFixtureFile(t, root, "data/jrrp/0.jpg", "jrrp-jpg")
	writeFixtureFile(t, root, "data/maimaidx/static.old/HanYi.ttf", "font-data")
	writeFixtureFile(t, root, "data/stickers/test.webp", "sticker-data")
	writeFixtureFile(t, root, "data/randpic/dly/pic1.jpg", "randpic-data")
	writeFixtureFile(t, root, "data/randpic/fu/legacy.png", "same-image")
	writeFixtureFile(t, root, "data/sayings/fu/legacy.png", "same-image")
	writeFixtureFile(t, root, "data/guesssong/group_1/temp.mp3", "runtime-audio")
	writeFixtureFile(t, root, "data/sayings/archive.tgz", "archive-noise")

	return root
}

func writeFixtureFile(t *testing.T, root, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s failed: %v", fullPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s failed: %v", fullPath, err)
	}
}

func assertStats(t *testing.T, stats *Stats, jsonFiles, binaryFiles, cpStories, meals, ktvSongs, luckTemplates, guessSongs, randPicItems int) {
	t.Helper()

	if stats == nil {
		t.Fatalf("stats is nil")
	}
	if stats.JSONFilesArchived != jsonFiles ||
		stats.BinaryFilesArchived != binaryFiles ||
		stats.CPStories != cpStories ||
		stats.MealEntries != meals ||
		stats.KTVSongs != ktvSongs ||
		stats.LuckTemplates != luckTemplates ||
		stats.GuessSongCatalog != guessSongs ||
		stats.RandPicItems != randPicItems {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.SkippedFiles < 5 {
		t.Fatalf("expected skipped files to be recorded, got %+v", stats)
	}
}

func assertTableCount(t *testing.T, tx *gorm.DB, model any, want int64) {
	t.Helper()

	var count int64
	if err := tx.Model(model).Count(&count).Error; err != nil {
		t.Fatalf("count %T failed: %v", model, err)
	}
	if count != want {
		t.Fatalf("unexpected %T count: got %d want %d", model, count, want)
	}
}
