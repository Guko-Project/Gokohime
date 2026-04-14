package dbtest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	"gorm.io/gorm"
)

func RepoRoot(t testing.TB) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve caller path failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func Open(t testing.TB) *gorm.DB {
	t.Helper()

	if dsn := strings.TrimSpace(os.Getenv("GOKOHIME_TEST_DSN")); dsn != "" {
		db, err := database.Init(dsn)
		if err != nil {
			t.Skipf("skip integration test: init database from GOKOHIME_TEST_DSN failed: %v", err)
		}
		return db
	}

	cfgPath := filepath.Join(RepoRoot(t), "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Skipf("skip integration test: load %s failed: %v", cfgPath, err)
	}

	db, err := database.Init(cfg.Database.DSN())
	if err != nil {
		t.Skipf("skip integration test: init database failed: %v", err)
	}
	return db
}

func BeginTx(t testing.TB) *gorm.DB {
	t.Helper()

	tx := Open(t).Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction failed: %v", tx.Error)
	}
	t.Cleanup(func() {
		_ = tx.Rollback().Error
	})
	return tx
}

func UseAsGlobal(t testing.TB, tx *gorm.DB) {
	t.Helper()

	restore := database.SwapGlobalForTest(tx)
	t.Cleanup(restore)
}

func Truncate(t testing.TB, tx *gorm.DB, tables ...string) {
	t.Helper()

	if len(tables) == 0 {
		return
	}
	stmt := "TRUNCATE TABLE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if err := tx.Exec(stmt).Error; err != nil {
		t.Fatalf("truncate tables failed: %v", err)
	}
}

func ResetModels(t testing.TB, tx *gorm.DB, models ...any) {
	t.Helper()

	cleaner := tx.Session(&gorm.Session{AllowGlobalUpdate: true})
	for _, model := range models {
		if err := cleaner.Unscoped().Delete(model).Error; err != nil {
			t.Fatalf("reset %T failed: %v", model, err)
		}
	}
}
