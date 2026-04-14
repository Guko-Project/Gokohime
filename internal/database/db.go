package database

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// Init initializes the PostgreSQL connection with pgvector support.
func Init(dsn string) (*gorm.DB, error) {
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	// Enable pgvector extension
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		log.Warnf("[db] failed to create vector extension: %v", err)
	}

	// Auto migrate all models
	if err := db.AutoMigrate(
		&UserProfile{},
		&Memory{},
		&Sticker{},
		&GuessGameSession{},
		&DailyLuck{},
		&PluginDataFile{},
		&PluginBinaryFile{},
		&MigrationRun{},
		&CPStory{},
		&MealEntry{},
		&KTVSong{},
		&DailyLuckTemplate{},
		&GuessSongCatalog{},
		&RandPicItem{},
		&PluginKV{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	// Create HNSW indexes for vector search
	db.Exec("CREATE INDEX IF NOT EXISTS idx_memory_embedding ON memories USING hnsw (embedding vector_cosine_ops)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_sticker_embedding ON stickers USING hnsw (embedding vector_cosine_ops)")

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Info("[db] PostgreSQL connected successfully")
	return db, nil
}

// Get returns the global database instance.
func Get() *gorm.DB {
	return db
}

// SwapGlobalForTest swaps the global DB handle and returns a restore function.
// It exists so integration tests can route plugin code through an isolated transaction.
func SwapGlobalForTest(testDB *gorm.DB) func() {
	previous := db
	db = testDB
	return func() {
		db = previous
	}
}
