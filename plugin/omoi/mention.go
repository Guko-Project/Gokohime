package omoi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
)

// messageText retains mention targets in prompts, history and memory evidence.
// Only the bot's own mention is omitted, preserving bare-mention handling.
func messageText(ctx *zero.Ctx) string {
	var text strings.Builder
	names := map[string]string{}
	lookupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, seg := range ctx.Event.Message {
		switch seg.Type {
		case "text":
			text.WriteString(seg.Data["text"])
		case "at":
			qq := seg.Data["qq"]
			if qq == "all" {
				text.WriteString("@全体成员")
				continue
			}
			id, err := strconv.ParseInt(qq, 10, 64)
			if err != nil || id <= 0 || id == ctx.Event.SelfID {
				continue
			}
			name, found := names[qq]
			if !found {
				if id == ctx.Event.UserID {
					name = getNickname(ctx)
				} else if ctx.Event.GroupID != 0 && lookupCtx.Err() == nil {
					info := ctx.CallActionWithContext(lookupCtx, "get_group_member_info", zero.Params{
						"group_id": ctx.Event.GroupID, "user_id": id, "no_cache": false,
					}).Data
					name = info.Get("card").String()
					if name == "" {
						name = info.Get("nickname").String()
					}
				}
				names[qq] = name
			}
			if name == "" || name == qq {
				fmt.Fprintf(&text, "@%s", qq)
			} else {
				fmt.Fprintf(&text, "@%s(%s)", name, qq)
			}
		}
	}
	return strings.TrimSpace(text.String())
}
