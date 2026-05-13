package database_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/database"
)

func TestSQLiteInitEnablesWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal-test.db")
	db, err := database.InitWithDialect(database.DialectSQLite, dbPath)
	if err != nil {
		t.Fatalf("InitWithDialect sqlite failed: %v", err)
	}

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal_mode failed: %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("expected sqlite WAL mode, got %q", journalMode)
	}
}
