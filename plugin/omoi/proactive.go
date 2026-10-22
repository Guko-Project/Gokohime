package omoi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// proactivePolicy is Omoi's effective unsolicited-reply policy for one group.
// Omoi runs the trigger chain (probability, judges) and applies cooldown and
// hourly caps; the connector only decides when to ask.
type proactivePolicy struct {
	Configured    bool       `json:"configured"`
	Enabled       bool       `json:"enabled"`
	MinMessages   int        `json:"min_messages"`
	QuietSeconds  int        `json:"quiet_seconds"`
	NextAllowedAt *time.Time `json:"next_allowed_at"`
	Overridden    bool       `json:"overridden"`
	Config        struct {
		CooldownSeconds *int  `json:"cooldown_seconds"`
		MaxPerHour      *int  `json:"max_per_hour"`
		Shadow          *bool `json:"shadow"`
		Stages          *[]struct {
			Type        string  `json:"type"`
			Probability float64 `json:"probability"`
			Backend     string  `json:"backend"`
			Threshold   float64 `json:"threshold"`
		} `json:"stages"`
	} `json:"config"`
}

const policyTTL = time.Minute

type cachedPolicy struct {
	policy *proactivePolicy // nil: Omoi has no policy endpoint or it failed
	at     time.Time
}

var policies sync.Map // group ID → cachedPolicy

func policyQuery(event *zero.Event) url.Values {
	return url.Values{
		"agent_id": {client.agentID},
		"instance": {"qq:" + strconv.FormatInt(event.SelfID, 10)},
		"target":   {"group:" + strconv.FormatInt(event.GroupID, 10)},
	}
}

// groupPolicy returns the cached policy, refreshing it at most once a minute.
// A nil result keeps the legacy config.yaml trigger.
func groupPolicy(ctx context.Context, event *zero.Event) *proactivePolicy {
	if client == nil {
		return nil
	}
	if v, ok := policies.Load(event.GroupID); ok && time.Since(v.(cachedPolicy).at) < policyTTL {
		return v.(cachedPolicy).policy
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var p proactivePolicy
	result := &p
	if err := client.deliveryAPI(ctx, http.MethodGet, "/proactive/policy?"+policyQuery(event).Encode(), nil, &p); err != nil {
		log.Debugf("[omoi] proactive policy unavailable for group %d: %v", event.GroupID, err)
		result = nil
	}
	policies.Store(event.GroupID, cachedPolicy{result, time.Now()})
	return result
}

func forgetPolicy(groupID int64) { policies.Delete(groupID) }

// observe buffers a message under an Omoi policy. It returns true when the
// caller should start an unsolicited turn now; with a quiet period it arms a
// debounce timer that calls fire instead.
func (b *GroupBuffer) observe(msg BufferedMessage, p *proactivePolicy, fire func()) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendLocked(msg)
	b.count++
	if !p.Enabled || b.firing || b.count < max(1, p.MinMessages) {
		return false
	}
	if p.NextAllowedAt != nil && time.Now().Before(*p.NextAllowedAt) {
		return false
	}
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if p.QuietSeconds > 0 {
		b.timer = time.AfterFunc(time.Duration(p.QuietSeconds)*time.Second, func() {
			if b.claim() {
				fire()
			}
		})
		return false
	}
	b.count, b.firing = 0, true
	return true
}

// claim reserves the buffer for one unsolicited turn.
func (b *GroupBuffer) claim() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.firing {
		return false
	}
	b.count, b.firing, b.timer = 0, true, nil
	return true
}

func (b *GroupBuffer) release() {
	b.mu.Lock()
	b.firing = false
	b.mu.Unlock()
}

// judgeText is the plain new chatter that Omoi's judges evaluate.
func judgeText(msgs []BufferedMessage) string {
	var sb strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&sb, "%s：%s\n", m.Nickname, m.Text)
	}
	return strings.TrimSpace(sb.String())
}

// handleActive shows or changes this group's override: .active [on|off|reset].
func handleActive(ctx *zero.Ctx) {
	ensureClient()
	if client == nil || ctx.Event.GroupID == 0 {
		ctx.Send("主动发言只在群聊中使用。")
		return
	}
	arg := strings.ToLower(strings.TrimSpace(fmt.Sprint(ctx.State["args"])))
	if arg == "<nil>" {
		arg = ""
	}
	reqCtx, cancel := context.WithTimeout(requestContext(), 15*time.Second)
	defer cancel()
	q := policyQuery(ctx.Event)
	var p proactivePolicy
	var err error
	switch arg {
	case "", "status":
		err = client.deliveryAPI(reqCtx, http.MethodGet, "/proactive/policy?"+q.Encode(), nil, &p)
	case "on", "off", "reset":
		if !groupAdmin(ctx) {
			ctx.Send("修改主动发言请由群主或管理员操作。")
			return
		}
		if arg == "reset" {
			err = client.deliveryAPI(reqCtx, http.MethodDelete, "/proactive/policy?"+q.Encode(), nil, &p)
		} else {
			body := map[string]any{"agent_id": q.Get("agent_id"), "instance": q.Get("instance"), "target": q.Get("target"), "config": map[string]bool{"enabled": arg == "on"}}
			err = client.deliveryAPI(reqCtx, http.MethodPut, "/proactive/policy", body, &p)
		}
		forgetPolicy(ctx.Event.GroupID)
	default:
		ctx.Send("用法：.active [on|off|reset]")
		return
	}
	if err != nil {
		ctx.Send(fmt.Sprintf("主动发言设置失败: %v", err))
		return
	}
	ctx.Send(describePolicy(&p))
}

func groupAdmin(ctx *zero.Ctx) bool {
	if ctx.Event.Sender != nil && (ctx.Event.Sender.Role == "owner" || ctx.Event.Sender.Role == "admin") {
		return true
	}
	for _, id := range config.Get().Bot.SuperUsers {
		if id == ctx.Event.UserID {
			return true
		}
	}
	return false
}

func describePolicy(p *proactivePolicy) string {
	if !p.Enabled {
		if !p.Configured {
			return "主动发言：关闭（Omoi Agent 未配置）"
		}
		return "主动发言：关闭"
	}
	lines := []string{"主动发言：开启"}
	if p.Overridden {
		lines[0] += "（本群单独设置）"
	}
	var chain []string
	if p.Config.Stages != nil {
		for _, st := range *p.Config.Stages {
			if st.Type == "probability" {
				chain = append(chain, fmt.Sprintf("概率 %.0f%%", st.Probability*100))
			} else {
				chain = append(chain, fmt.Sprintf("%s 判断 ≥ %.2f", st.Backend, st.Threshold))
			}
		}
	}
	if len(chain) == 0 {
		chain = []string{"无（交给模型自行决定）"}
	}
	lines = append(lines, "触发链路："+strings.Join(chain, " → "))
	var limits []string
	if p.MinMessages > 0 {
		limits = append(limits, fmt.Sprintf("至少 %d 条新消息", p.MinMessages))
	}
	if p.QuietSeconds > 0 {
		limits = append(limits, fmt.Sprintf("安静 %d 秒", p.QuietSeconds))
	}
	if v := p.Config.CooldownSeconds; v != nil && *v > 0 {
		limits = append(limits, fmt.Sprintf("冷却 %d 秒", *v))
	}
	if v := p.Config.MaxPerHour; v != nil && *v > 0 {
		limits = append(limits, fmt.Sprintf("每小时最多 %d 次", *v))
	}
	if len(limits) > 0 {
		lines = append(lines, "限制："+strings.Join(limits, "，"))
	}
	if p.Config.Shadow != nil && *p.Config.Shadow {
		lines = append(lines, "影子模式：只记录判断结果，不拦截")
	}
	if p.NextAllowedAt != nil && time.Now().Before(*p.NextAllowedAt) {
		lines = append(lines, "下次可发言："+p.NextAllowedAt.Local().Format("15:04"))
	}
	return strings.Join(lines, "\n")
}
