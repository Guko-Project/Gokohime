package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot      BotConfig      `yaml:"bot"`
	Admin    AdminConfig    `yaml:"admin"`
	Database DatabaseConfig `yaml:"database"`
	Banana   BananaConfig   `yaml:"banana"`
	RandPic  RandPicConfig  `yaml:"randpic"`
	Repeater RepeaterConfig `yaml:"repeater"`
	Omoi     OmoiConfig     `yaml:"omoi"`
	AceStep  AceStepConfig  `yaml:"ace_step"`
}

type AceStepConfig struct {
	APIKey          string `yaml:"api_key"`
	BaseURL         string `yaml:"base_url"`
	MaxDuration     int    `yaml:"max_duration"`
	DefaultDuration int    `yaml:"default_duration"`
	PollIntervalSec int    `yaml:"poll_interval_sec"`
	PollTimeoutSec  int    `yaml:"poll_timeout_sec"`
}

type OmoiConfig struct {
	Address            string   `yaml:"address"`
	APIKey             string   `yaml:"api_key"`
	AgentID            string   `yaml:"agent_id"`
	TimeoutSec         int      `yaml:"timeout_sec"`
	MaxMessageLen      int      `yaml:"max_message_len"`
	BufferSize         int      `yaml:"buffer_size"`
	TriggerCount       int      `yaml:"trigger_count"`
	TriggerIntervalSec int      `yaml:"trigger_interval_sec"`
	TriggerProbability float64  `yaml:"trigger_probability"`
	EnabledGroups      []int64  `yaml:"enabled_groups"`
	SkipMarker         string   `yaml:"skip_marker"`
	SplitMarker        string   `yaml:"split_marker"`
	TypingDelayMs      int      `yaml:"typing_delay_ms"`
	MaxTypingDelaySec  int      `yaml:"max_typing_delay_sec"`
	SkipPrefixes       []string `yaml:"skip_prefixes"`
}

type BotConfig struct {
	RWSURL            string  `yaml:"rws_url"`
	AccessToken       string  `yaml:"access_token"`
	CommandPrefix     string  `yaml:"command_prefix"`
	Nickname          string  `yaml:"nickname"`
	SuperUsers        []int64 `yaml:"super_users"`
	RingLen           uint    `yaml:"ring_len"`
	LatencyMS         uint    `yaml:"latency_ms"`
	MaxProcessTimeMin uint    `yaml:"max_process_time_min"`
}

type AdminConfig struct {
	Listen          string `yaml:"listen"`
	JWTSecret       string `yaml:"jwt_secret"`
	StaticDir       string `yaml:"static_dir"`
	InitialUsername string `yaml:"initial_username"`
	InitialPassword string `yaml:"initial_password"`
}

type DatabaseConfig struct {
	Dialect  string `yaml:"dialect"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

func (d *DatabaseConfig) Driver() string {
	dialect := strings.ToLower(strings.TrimSpace(d.Dialect))
	if dialect != "" {
		return dialect
	}
	if strings.TrimSpace(d.URL) != "" {
		if looksLikePostgresDSN(d.URL) {
			return "postgres"
		}
		return "sqlite"
	}
	if d.Host != "" || d.Port != 0 || d.User != "" || d.DBName != "" {
		return "postgres"
	}
	return "sqlite"
}

func (d *DatabaseConfig) DSN() string {
	if strings.TrimSpace(d.URL) != "" {
		return d.URL
	}
	if d.Driver() == "sqlite" {
		if strings.TrimSpace(d.Path) != "" {
			return d.Path
		}
		return "data/gokohime.db"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode)
}

func looksLikePostgresDSN(dsn string) bool {
	dsn = strings.ToLower(strings.TrimSpace(dsn))
	return strings.HasPrefix(dsn, "postgres://") ||
		strings.HasPrefix(dsn, "postgresql://") ||
		strings.Contains(dsn, "host=") ||
		strings.Contains(dsn, "dbname=")
}

type BananaConfig struct {
	APIKey            string `yaml:"api_key"`
	APIBase           string `yaml:"api_base"`
	APIMode           string `yaml:"api_mode"`
	Model             string `yaml:"model"`
	ProModel          string `yaml:"pro_model"`
	TimeoutSec        int    `yaml:"timeout_sec"`
	MaxRetries        int    `yaml:"max_retries"`
	CooldownSeconds   int    `yaml:"cooldown_seconds"`
	ResultFormat      string `yaml:"result_format"`
	FailureReply      string `yaml:"failure_reply"`
	ImageSize         string `yaml:"image_size"`
	GIModel           string `yaml:"gi_model"`
	GISize            string `yaml:"gi_size"`
	GIPollIntervalSec int    `yaml:"gi_poll_interval_sec"`
}

type RandPicConfig struct {
	CommandList    []string `yaml:"command_list"`
	StoreDirPath   string   `yaml:"store_dir_path"`
	DisabledGroups []string `yaml:"disabled_groups"`
}

type RepeaterConfig struct {
	Groups           []string `yaml:"groups"`
	MinMessageLength int      `yaml:"min_message_length"`
	MinMessageTimes  int      `yaml:"min_message_times"`
	MaxRepeatTime    int      `yaml:"max_repeat_time"`
	Blacklist        []string `yaml:"blacklist"`
}

var globalConfig *Config

// Load reads the config file and returns a Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Override with environment variables
	if key := os.Getenv("BANANA_API_KEY"); key != "" && cfg.Banana.APIKey == "" {
		cfg.Banana.APIKey = key
	}
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		cfg.Database.URL = dsn
	}
	if token := os.Getenv("ONEBOT_ACCESS_TOKEN"); token != "" && cfg.Bot.AccessToken == "" {
		cfg.Bot.AccessToken = token
	}
	if listen := os.Getenv("ADMIN_LISTEN"); listen != "" && cfg.Admin.Listen == "" {
		cfg.Admin.Listen = listen
	}
	if secret := os.Getenv("ADMIN_JWT_SECRET"); secret != "" && cfg.Admin.JWTSecret == "" {
		cfg.Admin.JWTSecret = secret
	}
	if staticDir := os.Getenv("ADMIN_STATIC_DIR"); staticDir != "" && cfg.Admin.StaticDir == "" {
		cfg.Admin.StaticDir = staticDir
	}
	if username := os.Getenv("ADMIN_INITIAL_USERNAME"); username != "" && cfg.Admin.InitialUsername == "" {
		cfg.Admin.InitialUsername = username
	}
	if password := os.Getenv("ADMIN_INITIAL_PASSWORD"); password != "" && cfg.Admin.InitialPassword == "" {
		cfg.Admin.InitialPassword = password
	}
	if cfg.Bot.RWSURL == "" {
		host := strings.TrimSpace(os.Getenv("HOST"))
		port := strings.TrimSpace(os.Getenv("PORT"))
		if host != "" && port != "" {
			cfg.Bot.RWSURL = fmt.Sprintf("ws://%s:%s", host, port)
		}
	}
	if len(cfg.Bot.SuperUsers) == 0 {
		cfg.Bot.SuperUsers = append(cfg.Bot.SuperUsers, parseInt64ListEnv("SUPERUSERS")...)
		if len(cfg.Bot.SuperUsers) == 0 {
			cfg.Bot.SuperUsers = append(cfg.Bot.SuperUsers, parseInt64ListEnv("GLOBAL_SUPERUSER")...)
		}
	}
	if len(cfg.RandPic.CommandList) == 0 {
		cfg.RandPic.CommandList = parseStringListEnv("randpic_command_list")
	}
	if cfg.RandPic.StoreDirPath == "" {
		if v := strings.TrimSpace(os.Getenv("randpic_store_dir_path")); v != "" {
			cfg.RandPic.StoreDirPath = v
		}
	}
	if len(cfg.RandPic.DisabledGroups) == 0 {
		cfg.RandPic.DisabledGroups = parseStringListEnv("randpic_banner_group")
	}
	if len(cfg.Repeater.Groups) == 0 {
		cfg.Repeater.Groups = parseStringListEnv("repeater_group")
	}
	if cfg.Repeater.MinMessageLength <= 0 {
		cfg.Repeater.MinMessageLength = parseIntEnv("repeater_min_message_length")
	}
	if cfg.Repeater.MinMessageTimes <= 0 {
		cfg.Repeater.MinMessageTimes = parseIntEnv("repeater_min_message_times")
	}
	if cfg.Repeater.MaxRepeatTime <= 0 {
		cfg.Repeater.MaxRepeatTime = parseIntEnv("repeater_max_repeat_time")
	}
	if len(cfg.Repeater.Blacklist) == 0 {
		cfg.Repeater.Blacklist = parseStringListEnv("repeater_blacklist")
	}
	if mode := os.Getenv("BANANA_API_MODE"); mode != "" && cfg.Banana.APIMode == "" {
		cfg.Banana.APIMode = mode
	}
	if model := os.Getenv("BANANA_MODEL"); model != "" && cfg.Banana.Model == "" {
		cfg.Banana.Model = model
	}
	if model := os.Getenv("BANANA_PRO_MODEL"); model != "" && cfg.Banana.ProModel == "" {
		cfg.Banana.ProModel = model
	}
	if apiBase := os.Getenv("BANANA_API_BASE"); apiBase != "" && cfg.Banana.APIBase == "" {
		cfg.Banana.APIBase = apiBase
	}
	if size := os.Getenv("BANANA_IMAGE_SIZE"); size != "" && cfg.Banana.ImageSize == "" {
		cfg.Banana.ImageSize = size
	}
	if cfg.Banana.TimeoutSec <= 0 {
		cfg.Banana.TimeoutSec = parseIntLikeEnv("BANANA_TIMEOUT")
	}
	if cfg.Banana.MaxRetries <= 0 {
		cfg.Banana.MaxRetries = parseIntEnv("BANANA_MAX_RETRIES")
	}
	if cfg.Banana.CooldownSeconds <= 0 {
		cfg.Banana.CooldownSeconds = parseIntEnv("BANANA_COOLDOWN_SECONDS")
	}
	if format := os.Getenv("BANANA_RESULT_FORMAT"); format != "" && cfg.Banana.ResultFormat == "" {
		cfg.Banana.ResultFormat = format
	}
	if cfg.Banana.APIMode == "" {
		cfg.Banana.APIMode = "openai"
	}
	if cfg.Banana.TimeoutSec <= 0 {
		cfg.Banana.TimeoutSec = 300
	}
	if cfg.Banana.MaxRetries < 0 {
		cfg.Banana.MaxRetries = 0
	}
	if cfg.Banana.CooldownSeconds < 0 {
		cfg.Banana.CooldownSeconds = 0
	}
	if cfg.Banana.ResultFormat == "" {
		cfg.Banana.ResultFormat = "b64_json"
	}
	if cfg.Banana.FailureReply == "" {
		cfg.Banana.FailureReply = "图片生成失败了，请稍后再试。"
	}
	if cfg.Banana.ImageSize == "" {
		cfg.Banana.ImageSize = "1024x1024"
	}
	if cfg.RandPic.StoreDirPath == "" {
		cfg.RandPic.StoreDirPath = "data/randpic"
	}
	if cfg.Repeater.MinMessageLength <= 0 {
		cfg.Repeater.MinMessageLength = 1
	}
	if cfg.Repeater.MinMessageTimes <= 0 {
		cfg.Repeater.MinMessageTimes = 2
	}
	if cfg.Repeater.MaxRepeatTime <= 0 {
		cfg.Repeater.MaxRepeatTime = 2
	}
	if cfg.Admin.Listen == "" {
		cfg.Admin.Listen = "127.0.0.1:8080"
	}
	if cfg.Admin.StaticDir == "" {
		cfg.Admin.StaticDir = "web/dist"
	}
	if cfg.Admin.InitialUsername == "" {
		cfg.Admin.InitialUsername = "admin"
	}

	// AceStep defaults + env overrides
	if key := os.Getenv("ACE_STEP_API_KEY"); key != "" && cfg.AceStep.APIKey == "" {
		cfg.AceStep.APIKey = key
	}
	if base := os.Getenv("ACE_STEP_BASE_URL"); base != "" && cfg.AceStep.BaseURL == "" {
		cfg.AceStep.BaseURL = base
	}
	if cfg.AceStep.MaxDuration <= 0 {
		cfg.AceStep.MaxDuration = 60
	}
	if cfg.AceStep.DefaultDuration <= 0 {
		cfg.AceStep.DefaultDuration = 30
	}
	if cfg.AceStep.PollIntervalSec <= 0 {
		cfg.AceStep.PollIntervalSec = 3
	}
	if cfg.AceStep.PollTimeoutSec <= 0 {
		cfg.AceStep.PollTimeoutSec = 300
	}

	// Omoi defaults + env overrides
	if addr := os.Getenv("OMOI_ADDRESS"); addr != "" && cfg.Omoi.Address == "" {
		cfg.Omoi.Address = addr
	}
	if key := os.Getenv("OMOI_API_KEY"); key != "" && cfg.Omoi.APIKey == "" {
		cfg.Omoi.APIKey = key
	}
	if agentID := os.Getenv("OMOI_AGENT_ID"); agentID != "" && cfg.Omoi.AgentID == "" {
		cfg.Omoi.AgentID = agentID
	}
	if cfg.Omoi.TimeoutSec <= 0 {
		cfg.Omoi.TimeoutSec = 120
	}
	if cfg.Omoi.MaxMessageLen <= 0 {
		cfg.Omoi.MaxMessageLen = 2000
	}
	if cfg.Omoi.BufferSize <= 0 {
		cfg.Omoi.BufferSize = 20
	}
	if cfg.Omoi.TriggerCount <= 0 {
		cfg.Omoi.TriggerCount = 15
	}
	if cfg.Omoi.TriggerIntervalSec <= 0 {
		cfg.Omoi.TriggerIntervalSec = 180
	}
	if cfg.Omoi.SkipMarker == "" {
		cfg.Omoi.SkipMarker = "[SKIP]"
	}
	if cfg.Omoi.SplitMarker == "" {
		cfg.Omoi.SplitMarker = "<<<SPLIT>>>"
	}
	if cfg.Omoi.TypingDelayMs <= 0 {
		cfg.Omoi.TypingDelayMs = 60
	}
	if cfg.Omoi.MaxTypingDelaySec <= 0 {
		cfg.Omoi.MaxTypingDelaySec = 4
	}
	if len(cfg.Omoi.SkipPrefixes) == 0 {
		cfg.Omoi.SkipPrefixes = []string{".", "/", "#"}
	}

	globalConfig = cfg
	return cfg, nil
}

// Get returns the global config. Must call Load first.
func Get() *Config {
	return globalConfig
}

func parseStringListEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		return values
	}

	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	values = make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, `"'`))
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func parseInt64ListEnv(key string) []int64 {
	items := parseStringListEnv(key)
	result := make([]int64, 0, len(items))
	for _, item := range items {
		value, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		if err == nil {
			result = append(result, value)
		}
	}
	return result
}

func parseIntEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func parseIntLikeEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	if value, err := strconv.Atoi(raw); err == nil {
		return value
	}
	floatValue, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return int(floatValue)
}
