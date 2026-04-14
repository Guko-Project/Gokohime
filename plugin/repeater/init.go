package repeater

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const repeatRollThreshold = 0.7

type groupState struct {
	mu          sync.Mutex
	lastMsg     string
	count       int
	repeatCount int
}

type runtimeConfig struct {
	allowAll         bool
	enabledGroups    map[string]struct{}
	blacklist        map[string]struct{}
	commandPrefixes  []string
	minMessageLength int
	minMessageTimes  int
	maxRepeatTime    int
}

var states sync.Map

func init() {
	rand.Seed(time.Now().UnixNano())

	zero.OnMessage(zero.OnlyGroup).SetPriority(99).SetBlock(false).Handle(handleRepeat)
}

func handleRepeat(ctx *zero.Ctx) {
	if ctx == nil || ctx.Event == nil {
		return
	}
	if ctx.Event.UserID != 0 && ctx.Event.UserID == ctx.Event.SelfID {
		return
	}

	cfg := loadRuntimeConfig(config.Get())
	raw := strings.TrimSpace(ctx.Event.RawMessage)
	if raw == "" {
		return
	}
	if !cfg.isEnabledGroup(ctx.Event.GroupID) || cfg.isBlacklisted(raw) || looksLikeCommandMessage(raw, cfg.commandPrefixes) {
		return
	}
	if !cfg.isRepeatCandidate(ctx.Event.Message, ctx.ExtractPlainText()) {
		return
	}

	val, _ := states.LoadOrStore(ctx.Event.GroupID, &groupState{})
	st := val.(*groupState)

	st.mu.Lock()
	defer st.mu.Unlock()

	if raw != st.lastMsg {
		st.lastMsg = raw
		st.count = 1
		st.repeatCount = 0
		return
	}

	st.count++
	if st.count < cfg.minMessageTimes || st.repeatCount >= cfg.maxRepeatTime {
		return
	}
	if rand.Float64() <= repeatRollThreshold {
		return
	}

	reply := buildRepeaterMessage(ctx.Event.Message)
	if len(reply) == 0 {
		return
	}

	st.repeatCount++
	ctx.Send(reply)
}

func loadRuntimeConfig(cfg *config.Config) runtimeConfig {
	result := runtimeConfig{
		enabledGroups:    make(map[string]struct{}),
		blacklist:        make(map[string]struct{}),
		commandPrefixes:  commandPrefixes(cfg),
		minMessageLength: 1,
		minMessageTimes:  2,
		maxRepeatTime:    2,
	}
	if cfg == nil {
		return result
	}

	if cfg.Repeater.MinMessageLength > 0 {
		result.minMessageLength = cfg.Repeater.MinMessageLength
	}
	if cfg.Repeater.MinMessageTimes > 0 {
		result.minMessageTimes = cfg.Repeater.MinMessageTimes
	}
	if cfg.Repeater.MaxRepeatTime > 0 {
		result.maxRepeatTime = cfg.Repeater.MaxRepeatTime
	}

	for _, group := range cfg.Repeater.Groups {
		group = strings.TrimSpace(strings.ToLower(group))
		if group == "" {
			continue
		}
		if group == "all" {
			result.allowAll = true
			continue
		}
		result.enabledGroups[group] = struct{}{}
	}

	for _, item := range cfg.Repeater.Blacklist {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		result.blacklist[item] = struct{}{}
	}

	return result
}

func commandPrefixes(cfg *config.Config) []string {
	seen := make(map[string]struct{})
	prefixes := make([]string, 0, 2)

	appendPrefix := func(prefix string) {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return
		}
		if _, ok := seen[prefix]; ok {
			return
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}

	appendPrefix(".")
	if cfg != nil {
		appendPrefix(cfg.Bot.CommandPrefix)
	}
	return prefixes
}

func (cfg runtimeConfig) isEnabledGroup(groupID int64) bool {
	if cfg.allowAll {
		return true
	}
	_, ok := cfg.enabledGroups[strconv.FormatInt(groupID, 10)]
	return ok
}

func (cfg runtimeConfig) isBlacklisted(raw string) bool {
	_, ok := cfg.blacklist[strings.TrimSpace(raw)]
	return ok
}

func (cfg runtimeConfig) isRepeatCandidate(msg message.Message, plainText string) bool {
	for _, seg := range msg {
		switch seg.Type {
		case "image", "face":
			return true
		}
	}
	return len([]rune(strings.TrimSpace(plainText))) >= cfg.minMessageLength
}

func looksLikeCommandMessage(raw string, prefixes []string) bool {
	raw = strings.TrimSpace(raw)
	for _, prefix := range prefixes {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(raw, prefix)) != ""
	}
	return false
}

func buildRepeaterMessage(src message.Message) message.Message {
	dst := make(message.Message, 0, len(src))
	for _, seg := range src {
		switch seg.Type {
		case "reply":
			continue
		case "text":
			dst = append(dst, message.Text(seg.Data["text"]))
		case "image":
			if url := seg.Data["url"]; url != "" {
				dst = append(dst, message.Image(url))
			} else {
				dst = append(dst, seg)
			}
		case "face":
			if id, err := strconv.Atoi(seg.Data["id"]); err == nil {
				dst = append(dst, message.Face(id))
			} else {
				dst = append(dst, seg)
			}
		default:
			dst = append(dst, seg)
		}
	}
	return dst
}
