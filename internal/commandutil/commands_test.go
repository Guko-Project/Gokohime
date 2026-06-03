package commandutil

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestMatchReplyableCommandWithReply(t *testing.T) {
	match, ok := MatchReplyableCommand(message.Message{
		message.Reply("42"),
		message.Text(".save test"),
	}, "save")
	if !ok {
		t.Fatalf("expected replyable command to match")
	}
	if match.Prefix != "." || match.Command != "save" || match.Args != "test" {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func TestMatchReplyableCommandWithSlashPrefix(t *testing.T) {
	match, ok := MatchReplyableCommand(message.Message{
		message.Text("/save"),
	}, "save")
	if !ok {
		t.Fatalf("expected slash-prefixed command to match")
	}
	if match.Prefix != "/" || match.Command != "save" || match.Args != "" {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func TestMatchReplyableCommandPrefersLongestCommand(t *testing.T) {
	match, ok := MatchReplyableCommand(message.Message{
		message.Text(".banana-pro 梦幻森林"),
	}, "banana", "banana-pro")
	if !ok {
		t.Fatalf("expected longest command to match")
	}
	if match.Command != "banana-pro" || match.Args != "梦幻森林" {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func TestMatchReplyableCommandWithReplyAndAt(t *testing.T) {
	match, ok := MatchReplyableCommand(message.Message{
		message.Reply("42"),
		message.Segment{Type: "at", Data: map[string]string{"qq": "123456"}},
		message.Text(".add dly"),
	}, "add")
	if !ok {
		t.Fatalf("expected replyable command to match with [reply, at, text]")
	}
	if match.Prefix != "." || match.Command != "add" || match.Args != "dly" {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func TestMatchReplyableCommandWithReplyAtAndImage(t *testing.T) {
	match, ok := MatchReplyableCommand(message.Message{
		message.Reply("42"),
		message.Segment{Type: "at", Data: map[string]string{"qq": "123456"}},
		message.Text(".add dly"),
		message.Image("http://example.com/img.png"),
	}, "add")
	if !ok {
		t.Fatalf("expected replyable command to match with [reply, at, text, image]")
	}
	if match.Prefix != "." || match.Command != "add" || match.Args != "dly" {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func TestMatchReplyableCommandNoTextSegment(t *testing.T) {
	_, ok := MatchReplyableCommand(message.Message{
		message.Reply("42"),
		message.Image("http://example.com/img.png"),
	}, "add")
	if ok {
		t.Fatalf("expected no match when there is no text segment")
	}
}

func TestReplyableCommandRuleSetsState(t *testing.T) {
	rule := ReplyableCommandRule("banana", "banana-pro")
	ctx := &zero.Ctx{
		Event: &zero.Event{
			Message: message.Message{
				message.Reply("7"),
				message.Text("/banana-pro test"),
			},
		},
	}

	if !rule(ctx) {
		t.Fatalf("expected rule to match")
	}
	if got := ctx.State["prefix"]; got != "/" {
		t.Fatalf("unexpected prefix in state: %#v", got)
	}
	if got := ctx.State["command"]; got != "banana-pro" {
		t.Fatalf("unexpected command in state: %#v", got)
	}
	if got := ctx.State["args"]; got != "test" {
		t.Fatalf("unexpected args in state: %#v", got)
	}
}
