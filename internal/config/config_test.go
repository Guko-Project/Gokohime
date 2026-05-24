package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadSupportsLegacyEnvMappings(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ONEBOT_ACCESS_TOKEN", "legacy-token")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "8080")
	t.Setenv("SUPERUSERS", `["10001","10002"]`)
	t.Setenv("randpic_command_list", `["dly","kgk"]`)
	t.Setenv("randpic_store_dir_path", "data/custom-randpic")
	t.Setenv("randpic_banner_group", `["123456"]`)
	t.Setenv("repeater_group", `["all","654321"]`)
	t.Setenv("repeater_min_message_length", "2")
	t.Setenv("repeater_min_message_times", "3")
	t.Setenv("repeater_max_repeat_time", "4")
	t.Setenv("repeater_blacklist", `["黑名单消息"]`)
	t.Setenv("BANANA_API_KEY", "legacy-banana-key")
	t.Setenv("BANANA_API_MODE", "custom")
	t.Setenv("BANANA_MODEL", "nano-banana-fast")
	t.Setenv("BANANA_PRO_MODEL", "nano-banana-pro")
	t.Setenv("BANANA_API_BASE", "https://banana.example")
	t.Setenv("BANANA_TIMEOUT", "120.0")
	t.Setenv("BANANA_MAX_RETRIES", "5")
	t.Setenv("BANANA_COOLDOWN_SECONDS", "15")
	t.Setenv("BANANA_RESULT_FORMAT", "url")

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bot: {}
database: {}
banana: {}
randpic: {}
repeater: {}
`), 0o644); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Bot.AccessToken != "legacy-token" {
		t.Fatalf("unexpected bot access token: %q", cfg.Bot.AccessToken)
	}
	if cfg.Bot.RWSURL != "ws://127.0.0.1:8080" {
		t.Fatalf("unexpected bot rws_url: %q", cfg.Bot.RWSURL)
	}
	if !reflect.DeepEqual(cfg.Bot.SuperUsers, []int64{10001, 10002}) {
		t.Fatalf("unexpected super users: %#v", cfg.Bot.SuperUsers)
	}

	if !reflect.DeepEqual(cfg.RandPic.CommandList, []string{"dly", "kgk"}) {
		t.Fatalf("unexpected randpic command list: %#v", cfg.RandPic.CommandList)
	}
	if cfg.RandPic.StoreDirPath != "data/custom-randpic" {
		t.Fatalf("unexpected randpic store dir: %q", cfg.RandPic.StoreDirPath)
	}
	if !reflect.DeepEqual(cfg.RandPic.DisabledGroups, []string{"123456"}) {
		t.Fatalf("unexpected randpic disabled groups: %#v", cfg.RandPic.DisabledGroups)
	}

	if !reflect.DeepEqual(cfg.Repeater.Groups, []string{"all", "654321"}) {
		t.Fatalf("unexpected repeater groups: %#v", cfg.Repeater.Groups)
	}
	if cfg.Repeater.MinMessageLength != 2 || cfg.Repeater.MinMessageTimes != 3 || cfg.Repeater.MaxRepeatTime != 4 {
		t.Fatalf("unexpected repeater limits: %+v", cfg.Repeater)
	}
	if !reflect.DeepEqual(cfg.Repeater.Blacklist, []string{"黑名单消息"}) {
		t.Fatalf("unexpected repeater blacklist: %#v", cfg.Repeater.Blacklist)
	}

	if cfg.Banana.APIKey != "legacy-banana-key" ||
		cfg.Banana.APIMode != "custom" ||
		cfg.Banana.Model != "nano-banana-fast" ||
		cfg.Banana.ProModel != "nano-banana-pro" ||
		cfg.Banana.APIBase != "https://banana.example" ||
		cfg.Banana.TimeoutSec != 120 ||
		cfg.Banana.MaxRetries != 5 ||
		cfg.Banana.CooldownSeconds != 15 ||
		cfg.Banana.ResultFormat != "url" {
		t.Fatalf("unexpected banana config: %+v", cfg.Banana)
	}
}

func TestDatabaseConfigDefaultsToSQLite(t *testing.T) {
	cfg := DatabaseConfig{}
	if got := cfg.Driver(); got != "sqlite" {
		t.Fatalf("unexpected default database driver: %q", got)
	}
	if got := cfg.DSN(); got != "data/gokohime.db" {
		t.Fatalf("unexpected default sqlite dsn: %q", got)
	}

	cfg.Path = "tmp/test.db"
	if got := cfg.DSN(); got != "tmp/test.db" {
		t.Fatalf("unexpected sqlite path dsn: %q", got)
	}
}

func TestDatabaseConfigSupportsPostgresCompatibility(t *testing.T) {
	cfg := DatabaseConfig{
		Dialect:  "postgres",
		Host:     "localhost",
		Port:     5432,
		User:     "gokohime",
		Password: "secret",
		DBName:   "gokohime",
		SSLMode:  "disable",
	}
	if got := cfg.Driver(); got != "postgres" {
		t.Fatalf("unexpected postgres driver: %q", got)
	}
	want := "host=localhost port=5432 user=gokohime password=secret dbname=gokohime sslmode=disable"
	if got := cfg.DSN(); got != want {
		t.Fatalf("unexpected postgres dsn: %q", got)
	}

	cfg = DatabaseConfig{URL: "postgres://user:pass@localhost:5432/gokohime?sslmode=disable"}
	if got := cfg.Driver(); got != "postgres" {
		t.Fatalf("unexpected url-inferred postgres driver: %q", got)
	}
}

func TestDatabaseURLOverridesLegacyPostgresFields(t *testing.T) {
	cfg := DatabaseConfig{
		URL:    "data/override.db",
		Host:   "localhost",
		Port:   5432,
		User:   "legacy",
		DBName: "legacy",
	}
	if got := cfg.Driver(); got != "sqlite" {
		t.Fatalf("unexpected url-overridden driver: %q", got)
	}
	if got := cfg.DSN(); got != "data/override.db" {
		t.Fatalf("unexpected url-overridden dsn: %q", got)
	}
}
