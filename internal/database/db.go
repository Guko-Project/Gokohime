package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

var db *gorm.DB

// Init initializes a database connection, inferring the dialect from the DSN.
func Init(dsn string) (*gorm.DB, error) {
	return InitWithDialect(inferDialect(dsn), dsn)
}

// InitWithDialect initializes a database connection for the given dialect.
func InitWithDialect(dialect, dsn string) (*gorm.DB, error) {
	dialect = normalizeDialect(dialect)

	var err error
	switch dialect {
	case DialectSQLite:
		db, err = openSQLite(dsn)
	case DialectPostgres:
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		})
	default:
		return nil, fmt.Errorf("unsupported database dialect %q", dialect)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s database: %w", dialect, err)
	}

	if err := db.AutoMigrate(coreModels()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	configurePool(sqlDB, dialect)

	log.Infof("[db] %s connected successfully", dialect)
	return db, nil
}

func openSQLite(dsn string) (*gorm.DB, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		dsn = "data/gokohime.db"
	}
	if isSQLiteFilePath(dsn) {
		dir := filepath.Dir(dsn)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite database directory: %w", err)
			}
		}
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return db, nil
}

func configurePool(sqlDB interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxLifetime(time.Duration)
}, dialect string) {
	if dialect == DialectSQLite {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(0)
		return
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
}

func coreModels() []any {
	return []any{
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
	}
}

func inferDialect(dsn string) string {
	dsn = strings.ToLower(strings.TrimSpace(dsn))
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") ||
		strings.Contains(dsn, "host=") || strings.Contains(dsn, "dbname=") {
		return DialectPostgres
	}
	return DialectSQLite
}

func normalizeDialect(dialect string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "", "sqlite", "sqlite3":
		return DialectSQLite
	case "postgres", "postgresql", "pg":
		return DialectPostgres
	default:
		return strings.ToLower(strings.TrimSpace(dialect))
	}
}

func isSQLiteFilePath(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" || trimmed == ":memory:" {
		return false
	}
	if strings.HasPrefix(trimmed, "file:") {
		return false
	}
	return !strings.Contains(trimmed, "?") || strings.HasSuffix(strings.Split(trimmed, "?")[0], ".db")
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
