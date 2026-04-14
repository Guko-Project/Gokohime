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
	t.Setenv("SIMPLE_GPT_API_KEY", "legacy-openai-key")
	t.Setenv("SIMPLE_GPT_MODEL", "legacy-model")
	t.Setenv("SIMPLE_GPT_API_BASE", "https://legacy.example/v1")
	t.Setenv("SIMPLE_GPT_PROACTIVE_GROUP_WHITELIST", `["111","222"]`)
	t.Setenv("SIMPLE_GPT_WEATHER_API_KEY", "legacy-weather-key")
	t.Setenv("SIMPLE_GPT_WEATHER_CITY", "110108")
	t.Setenv("SIMPLE_GPT_SEARCH_API_KEY", "legacy-search-key")
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
ai: {}
database: {}
weather: {}
search: {}
embedding: {}
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

	if cfg.AI.OpenAIAPIKey != "legacy-openai-key" || cfg.AI.OpenAIModel != "legacy-model" || cfg.AI.OpenAIAPIBase != "https://legacy.example/v1" {
		t.Fatalf("unexpected ai legacy config: %+v", cfg.AI)
	}
	if !reflect.DeepEqual(cfg.AI.ProactiveGroupWhitelist, []int64{111, 222}) {
		t.Fatalf("unexpected ai proactive whitelist: %#v", cfg.AI.ProactiveGroupWhitelist)
	}
	if cfg.Weather.APIKey != "legacy-weather-key" || cfg.Weather.DefaultCity != "110108" {
		t.Fatalf("unexpected weather config: %+v", cfg.Weather)
	}
	if cfg.Search.APIKey != "legacy-search-key" {
		t.Fatalf("unexpected search api key: %q", cfg.Search.APIKey)
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
