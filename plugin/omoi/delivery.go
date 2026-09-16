package omoi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

type channelKey struct{}
type channelContext struct {
	ActorID   string `json:"actor_id"`
	Proactive bool   `json:"proactive"`
	Instance  string `json:"-"`
	Target    string `json:"-"`
}

func channelRequest(ctx context.Context, event *zero.Event, proactive bool) context.Context {
	kind, id := "private", event.UserID
	if event.GroupID != 0 {
		kind = "group"
		id = event.GroupID
	}
	return context.WithValue(ctx, channelKey{}, channelContext{ActorID: strconv.FormatInt(event.UserID, 10), Proactive: proactive, Instance: "qq:" + strconv.FormatInt(event.SelfID, 10), Target: kind + ":" + strconv.FormatInt(id, 10)})
}
func (c *OmoiClient) deliveryAPI(ctx context.Context, method, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delivery API HTTP %d", resp.StatusCode)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
func (c *OmoiClient) bindChannel(ctx context.Context, session string) error {
	v, ok := ctx.Value(channelKey{}).(channelContext)
	if !ok {
		return nil
	}
	return c.deliveryAPI(ctx, http.MethodPut, "/sessions/"+session+"/delivery", map[string]string{"instance": v.Instance, "channel": "qq", "target": v.Target, "reply_instruction": withReplyFormat("", config.Get().Omoi)}, nil)
}

type scheduledDelivery struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	Instance   string `json:"instance"`
	Channel    string `json:"channel"`
	Target     string `json:"target"`
	Text       string `json:"text"`
	LeaseToken string `json:"lease_token"`
}
type deliveryProgress struct {
	Sent    int            `json:"sent"`
	Sending bool           `json:"sending"`
	State   string         `json:"state"`
	Parts   []deliveryPart `json:"parts,omitempty"`
}
type deliveryPart struct {
	Text  string
	Delay time.Duration
}

func deliveryParts(text string, cfg config.OmoiConfig) []deliveryPart {
	var parts []deliveryPart
	var delay time.Duration
	sendSplitReply(text, cfg, func(text string) { parts = append(parts, deliveryPart{Text: text, Delay: delay}); delay = 0 }, func(d time.Duration) { delay += d })
	return parts
}

// deliverParts persists the uncertain window before each external send. A crash
// in that window is reported as unknown instead of resending a possibly sent part.
func deliverParts(parts []deliveryPart, progress deliveryProgress, save func(deliveryProgress) error, check func() error, send func(string) bool, sleep func(time.Duration) error) string {
	if progress.State != "" {
		return progress.State
	}
	if progress.Sending {
		return "unknown"
	}
	if len(parts) == 0 {
		return "skipped"
	}
	for i := progress.Sent; i < len(parts); i++ {
		if err := sleep(parts[i].Delay); err != nil {
			return "retry"
		}
		if err := check(); err != nil {
			return "retry"
		}
		progress.Sending = true
		if err := save(progress); err != nil {
			return "retry"
		}
		if !send(parts[i].Text) {
			return "unknown"
		}
		progress.Sent = i + 1
		progress.Sending = false
		if err := save(progress); err != nil {
			return "unknown"
		}
	}
	progress.State = "sent"
	if err := save(progress); err != nil {
		return "unknown"
	}
	return "sent"
}

func (c *OmoiClient) receipt(ctx context.Context, d scheduledDelivery, state string) error {
	return c.deliveryAPI(ctx, http.MethodPost, "/deliveries/"+d.ID+"/receipt", map[string]string{"lease_token": d.LeaseToken, "state": state}, nil)
}
func (c *OmoiClient) deliver(ctx context.Context, bot *zero.Ctx, d scheduledDelivery) {
	progress := deliveryProgress{}
	key := "delivery:" + d.ID
	raw, err := database.GetPluginKV(ctx, nil, kvNamespace, key)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		_ = c.receipt(ctx, d, "retry")
		return
	}
	if raw != "" && json.Unmarshal([]byte(raw), &progress) != nil {
		_ = c.receipt(ctx, d, "unknown")
		return
	}
	save := func(v deliveryProgress) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return database.SetPluginKV(ctx, nil, kvNamespace, key, string(raw))
	}
	target := strings.SplitN(d.Target, ":", 2)
	if d.Channel != "qq" || len(target) != 2 {
		_ = c.receipt(ctx, d, "unknown")
		return
	}
	id, err := strconv.ParseInt(target[1], 10, 64)
	if err != nil || id <= 0 {
		_ = c.receipt(ctx, d, "unknown")
		return
	}
	if target[0] != "group" && target[0] != "private" {
		_ = c.receipt(ctx, d, "unknown")
		return
	}
	// Ensure the old mapping still points to this session after a chat reset.
	session, err := database.GetPluginKV(ctx, nil, kvNamespace, "session:"+target[0]+":"+target[1])
	if err != nil || session != d.SessionID {
		_ = c.receipt(ctx, d, "skipped")
		return
	}
	check := func() error { return c.receipt(ctx, d, "extend") }
	sleep := func(delay time.Duration) error {
		for delay > 0 {
			step := min(delay, 30*time.Second)
			timer := time.NewTimer(step)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			delay -= step
			if err := check(); err != nil {
				return err
			}
		}
		return nil
	}
	send := func(text string) bool {
		var result int64
		if target[0] == "group" {
			result = bot.SendGroupMessage(id, message.Text(text))
		} else {
			result = bot.SendPrivateMessage(id, message.Text(text))
		}
		return result != 0
	}
	if progress.Parts == nil {
		progress.Parts = deliveryParts(d.Text, config.Get().Omoi)
		if err := save(progress); err != nil {
			_ = c.receipt(ctx, d, "retry")
			return
		}
	}
	state := deliverParts(progress.Parts, progress, save, check, send, sleep)
	if err := c.receipt(ctx, d, state); err != nil {
		log.Warnf("[omoi] scheduled delivery acknowledgment failed: id=%s state=%s", d.ID, state)
	}
}

// StartDeliveryWorker is called after configuration and database initialization.
// RangeBot also works immediately after reconnect, without a new chat message.
func StartDeliveryWorker(ctx context.Context) {
	ensureClient()
	if client == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			zero.RangeBot(func(id int64, bot *zero.Ctx) bool {
				callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				var d *scheduledDelivery
				err := client.deliveryAPI(callCtx, http.MethodPost, "/deliveries/claim", map[string]string{"instance": "qq:" + strconv.FormatInt(id, 10)}, &d)
				cancel()
				if err == nil && d != nil {
					client.deliver(ctx, bot, *d)
				}
				return ctx.Err() == nil
			})
		}
	}()
}
