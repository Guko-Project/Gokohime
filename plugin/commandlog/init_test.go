package commandlog

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestDetectCommandInvocation(t *testing.T) {
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Text(".save "),
			},
		},
	}

	command, args, ok := detectCommandInvocation(ctx)
	if !ok {
		t.Fatalf("expected .save to be detected as command")
	}
	if command != "save" || args != "" {
		t.Fatalf("unexpected command detection result: command=%q args=%q", command, args)
	}
}

func TestDetectCommandInvocationWithReply(t *testing.T) {
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Reply("42"),
				message.Text(".save"),
			},
		},
	}

	command, args, ok := detectCommandInvocation(ctx)
	if !ok {
		t.Fatalf("expected reply command to be detected")
	}
	if command != "save" || args != "" {
		t.Fatalf("unexpected command detection result: command=%q args=%q", command, args)
	}
}

func TestDetectCommandInvocationWithSlash(t *testing.T) {
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Text("/save"),
			},
		},
	}

	command, args, ok := detectCommandInvocation(ctx)
	if !ok {
		t.Fatalf("expected slash command to be detected")
	}
	if command != "save" || args != "" {
		t.Fatalf("unexpected command detection result: command=%q args=%q", command, args)
	}
}

func TestDetectCommandInvocationPrefersLongestCommand(t *testing.T) {
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Text(".banana-pro test"),
			},
		},
	}

	command, args, ok := detectCommandInvocation(ctx)
	if !ok {
		t.Fatalf("expected banana-pro to be detected")
	}
	if command != "banana-pro" || args != "test" {
		t.Fatalf("unexpected command detection result: command=%q args=%q", command, args)
	}
}
func TestDetectCommandInvocationNonCommand(t *testing.T) {
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Text("普通聊天内容"),
			},
		},
	}

	if command, args, ok := detectCommandInvocation(ctx); ok {
		t.Fatalf("did not expect plain text to be detected as command, got command=%q args=%q", command, args)
	}
}
