package omoi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
	OutOfScope    bool       `json:"out_of_scope"`
	// Triggers are alternative ways to start a turn, each with its own
	// timing. Older Omoi versions send none: one trigger uses the top level.
	Triggers []proactiveTrigger `json:"triggers"`
	Config   struct {
		CooldownSeconds *int            `json:"cooldown_seconds"`
		MaxPerHour      *int            `json:"max_per_hour"`
		MaxPerDay       *int            `json:"max_per_day"`
		Shadow          *bool           `json:"shadow"`
		Stages          *[]policyStage  `json:"stages"`
		Triggers        *[]policyChains `json:"triggers"`
	} `json:"config"`
}

type proactiveTrigger struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	MinMessages  int      `json:"min_messages"`
	QuietSeconds int      `json:"quiet_seconds"`
	BypassLimits bool     `json:"bypass_limits"`
	Keywords     []string `json:"keywords"`
	Regex        bool     `json:"regex"`
}

type policyStage struct {
	Type        string   `json:"type"`
	Probability float64  `json:"probability"`
	Backend     string   `json:"backend"`
	Threshold   float64  `json:"threshold"`
	Dimension   string   `json:"dimension"`
	Patterns    []string `json:"patterns"`
}

type policyChains struct {
	ID       string        `json:"id"`
	Label    string        `json:"label"`
	Disabled bool          `json:"disabled"`
	Stages   []policyStage `json:"stages"`
}

// triggers returns the policy's triggers; an empty ID means "ask without one".
func (p *proactivePolicy) triggers() []proactiveTrigger {
	if len(p.Triggers) > 0 {
		return p.Triggers
	}
	return []proactiveTrigger{{MinMessages: p.MinMessages, QuietSeconds: p.QuietSeconds}}
}

// ready reports whether a trigger may ask now under the shared speaking budget.
func (p *proactivePolicy) ready(tr proactiveTrigger, now time.Time) bool {
	return p.Enabled && (tr.BypassLimits || p.NextAllowedAt == nil || !now.Before(*p.NextAllowedAt))
}

// matchKeywords mirrors Omoi's keyword stage: any pattern, ignoring case.
func matchKeywords(patterns []string, regex bool, text string) bool {
	lower := strings.ToLower(text)
	for _, p := range patterns {
		if regex {
			if re, err := regexp.Compile("(?i)" + p); err == nil && re.MatchString(text) {
				return true
			}
		} else if p != "" && strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
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

// observe buffers a message under an Omoi policy and counts it for every
// trigger. It returns the trigger whose turn the caller should start now;
// triggers with a quiet period arm a debounce timer that calls fire instead.
func (b *GroupBuffer) observe(msg BufferedMessage, p *proactivePolicy, fire func(triggerID string)) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendLocked(msg)
	if b.counts == nil {
		b.counts, b.matched = map[string]int{}, map[string]bool{}
	}
	if b.timers == nil {
		b.timers = map[string]*time.Timer{}
	}
	triggers := p.triggers()
	for _, tr := range triggers {
		b.counts[tr.ID]++
		if len(tr.Keywords) > 0 && matchKeywords(tr.Keywords, tr.Regex, msg.Nickname+"："+msg.Text) {
			b.matched[tr.ID] = true
		}
	}
	if b.firing {
		return "", false
	}
	now := time.Now()
	for _, tr := range triggers {
		if b.counts[tr.ID] < max(1, tr.MinMessages) || (len(tr.Keywords) > 0 && !b.matched[tr.ID]) || !p.ready(tr, now) {
			continue
		}
		id := tr.ID
		if old := b.timers[id]; old != nil {
			old.Stop()
		}
		if tr.QuietSeconds > 0 {
			b.timers[id] = time.AfterFunc(time.Duration(tr.QuietSeconds)*time.Second, func() {
				if b.claim(id) {
					fire(id)
				}
			})
			continue
		}
		delete(b.timers, id)
		b.firing = true
		b.resetLocked(id)
		return id, true
	}
	return "", false
}

// claim reserves the buffer for one unsolicited turn of a trigger.
func (b *GroupBuffer) claim(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.firing {
		return false
	}
	b.firing = true
	delete(b.timers, id)
	b.resetLocked(id)
	return true
}

// resetLocked forgets a trigger's progress.
func (b *GroupBuffer) resetLocked(id string) {
	delete(b.counts, id)
	delete(b.matched, id)
}

// resetTriggers restarts every trigger's count once the agent has answered.
func (b *GroupBuffer) resetTriggers() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts, b.matched = nil, nil
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
		switch {
		case !p.Configured:
			return "主动发言：关闭（Omoi Agent 未配置）"
		case p.OutOfScope:
			return "主动发言：关闭（本群不在 Agent 的生效范围内，可用 .active on 单独开启）"
		}
		return "主动发言：关闭"
	}
	lines := []string{"主动发言：开启"}
	if p.Overridden {
		lines[0] += "（本群单独设置）"
	}
	if chains := p.Config.Triggers; chains != nil {
		if p.Config.Stages != nil && len(*p.Config.Stages) > 0 {
			lines = append(lines, "前置条件："+chainText(*p.Config.Stages))
		}
		timing := map[string]proactiveTrigger{}
		for _, tr := range p.Triggers {
			timing[tr.ID] = tr
		}
		lines = append(lines, "触发器（任一通过即发言）：")
		n := 0
		for _, c := range *chains {
			if c.Disabled {
				continue
			}
			n++
			name := c.Label
			if name == "" {
				name = c.ID
			}
			line := fmt.Sprintf("%d. %s：%s", n, name, chainText(c.Stages))
			var when []string
			if tr := timing[c.ID]; tr.MinMessages > 1 {
				when = append(when, fmt.Sprintf("%d 条新消息", tr.MinMessages))
			}
			if tr := timing[c.ID]; tr.QuietSeconds > 0 {
				when = append(when, fmt.Sprintf("安静 %d 秒", tr.QuietSeconds))
			}
			if timing[c.ID].BypassLimits {
				when = append(when, "不受冷却限制")
			}
			if len(when) > 0 {
				line += "（" + strings.Join(when, "，") + "）"
			}
			lines = append(lines, line)
		}
		if n == 0 {
			lines = append(lines, "（没有启用的触发器，不会主动发言）")
		}
	} else {
		var stages []policyStage
		if p.Config.Stages != nil {
			stages = *p.Config.Stages
		}
		lines = append(lines, "触发链路："+chainText(stages))
	}
	var limits []string
	if p.Config.Triggers == nil {
		if p.MinMessages > 0 {
			limits = append(limits, fmt.Sprintf("至少 %d 条新消息", p.MinMessages))
		}
		if p.QuietSeconds > 0 {
			limits = append(limits, fmt.Sprintf("安静 %d 秒", p.QuietSeconds))
		}
	}
	if v := p.Config.CooldownSeconds; v != nil && *v > 0 {
		limits = append(limits, fmt.Sprintf("冷却 %d 秒", *v))
	}
	if v := p.Config.MaxPerHour; v != nil && *v > 0 {
		limits = append(limits, fmt.Sprintf("每小时最多 %d 次", *v))
	}
	if v := p.Config.MaxPerDay; v != nil && *v > 0 {
		limits = append(limits, fmt.Sprintf("每天最多 %d 次", *v))
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

func chainText(stages []policyStage) string {
	var chain []string
	for _, st := range stages {
		switch st.Type {
		case "probability":
			chain = append(chain, fmt.Sprintf("概率 %.0f%%", st.Probability*100))
		case "keyword":
			chain = append(chain, "关键词「"+strings.Join(st.Patterns, "/")+"」")
		case "mood":
			chain = append(chain, "心情 "+st.Dimension)
		default:
			chain = append(chain, fmt.Sprintf("%s 判断 ≥ %.2f", st.Backend, st.Threshold))
		}
	}
	if len(chain) == 0 {
		return "无（交给模型自行决定）"
	}
	return strings.Join(chain, " → ")
}
