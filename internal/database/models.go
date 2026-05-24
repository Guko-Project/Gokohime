package database

import (
	"time"

	"gorm.io/gorm"
)

// GuessGameSession tracks active song guessing games.
type GuessGameSession struct {
	gorm.Model
	GroupID     int64     `gorm:"uniqueIndex"`
	Library     string    `gorm:"size:16;index"`
	SongID      int64     `gorm:"index"`
	SongName    string    `gorm:"size:255"`
	Artist      string    `gorm:"size:255"`
	AudioURL    string    `gorm:"size:1024"`
	Slice1Path  string    `gorm:"size:1024"`
	Slice2Path  string    `gorm:"size:1024"`
	HintStage   int       `gorm:"default:0"`
	AnswerCount int       `gorm:"default:0"`
	Answer      string    `gorm:"size:255"`
	PreparedAt  time.Time `gorm:"index"`
	LastError   string    `gorm:"type:text"`
	Active      bool      `gorm:"default:true"`
}

// DailyLuck stores daily fortune results per user.
type DailyLuck struct {
	gorm.Model
	UserID int64  `gorm:"uniqueIndex:idx_daily_luck_user_date"`
	Date   string `gorm:"uniqueIndex:idx_daily_luck_user_date;size:10"` // "2026-03-28"
	Score  int
	Text   string `gorm:"type:text"`
}

// PluginDataFile stores raw JSON assets imported from the /data tree.
type PluginDataFile struct {
	gorm.Model
	SourcePath   string `gorm:"uniqueIndex;size:1024"`
	DataCategory string `gorm:"index;size:64"`
	SHA256       string `gorm:"index;size:64"`
	FileSize     int64
	MimeType     string `gorm:"size:255"`
	RawJSON      string `gorm:"type:text"`
}

// PluginBinaryFile stores raw non-JSON assets imported from the /data tree.
type PluginBinaryFile struct {
	gorm.Model
	SourcePath   string `gorm:"uniqueIndex;size:1024"`
	DataCategory string `gorm:"index;size:64"`
	SHA256       string `gorm:"index;size:64"`
	FileSize     int64
	MimeType     string `gorm:"size:255"`
	RawContent   []byte
}

// MigrationRun stores the result of a data migration execution.
type MigrationRun struct {
	gorm.Model
	Name      string `gorm:"index;size:128"`
	Status    string `gorm:"index;size:32"`
	Summary   string `gorm:"type:text"`
	StatsJSON string `gorm:"type:text"`
}

// CPStory stores the CP short-story templates.
type CPStory struct {
	gorm.Model
	Story     string `gorm:"type:text;not null"`
	StoryHash string `gorm:"uniqueIndex;size:64"`
}

// MealEntry stores meal candidates grouped by command kind.
type MealEntry struct {
	gorm.Model
	Kind      string `gorm:"index;size:32"`
	Name      string `gorm:"size:512;not null"`
	EntryHash string `gorm:"uniqueIndex;size:64"`
}

// KTVSong stores the KTV song pool.
type KTVSong struct {
	gorm.Model
	Name      string `gorm:"index;size:255;not null"`
	Category  string `gorm:"index;size:128"`
	BV        string `gorm:"size:1024"`
	Issuer    string `gorm:"size:64"`
	EntryHash string `gorm:"uniqueIndex;size:64"`
}

// DailyLuckTemplate stores the fortune text/image templates.
type DailyLuckTemplate struct {
	gorm.Model
	Number   int    `gorm:"uniqueIndex"`
	Text     string `gorm:"type:text;not null"`
	ImageURL string `gorm:"size:1024"`
}

// GuessSongCatalog stores song IDs for each library.
type GuessSongCatalog struct {
	gorm.Model
	Library  string `gorm:"uniqueIndex:idx_guess_song_catalog;size:16"`
	SongID   int64  `gorm:"uniqueIndex:idx_guess_song_catalog"`
	MetaJSON string `gorm:"type:text"`
}

// RandPicItem stores image metadata for random picture categories.
type RandPicItem struct {
	gorm.Model
	Category     string `gorm:"uniqueIndex:idx_randpic_category_hash;index;size:128"`
	FileName     string `gorm:"size:255"`
	FilePath     string `gorm:"size:1024"`
	FileHash     string `gorm:"uniqueIndex:idx_randpic_category_hash;index;size:64"`
	KeywordsJSON string `gorm:"type:text"`
}

// PluginKV stores lightweight plugin configuration/state.
type PluginKV struct {
	gorm.Model
	Namespace string `gorm:"uniqueIndex:idx_plugin_kv;size:64"`
	Key       string `gorm:"uniqueIndex:idx_plugin_kv;size:128"`
	Value     string `gorm:"type:text"`
}
