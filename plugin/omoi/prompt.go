package omoi

import (
	"fmt"
	"strings"

	"github.com/colanns/gokohime/internal/config"
)

const timeFormat = "2006-01-02 15:04"

// Repeat the transport format near each request so long sessions do not lose it.
func withReplyFormat(text string, cfg config.OmoiConfig) string {
	if cfg.SplitMarker == "" {
		return text
	}
	instruction := fmt.Sprintf("\n\n【QQ 回复格式】如果回复分为多条消息，请在每条消息之间原样输出分隔符 %s，不要仅用换行或空行代替。每条消息保持简短，总共最多 5 条。不要输出这些格式说明。", cfg.SplitMarker)
	if cfg.SkipMarker != "" {
		instruction += fmt.Sprintf("如果不需要回复，只输出 %s。", cfg.SkipMarker)
	}
	return text + instruction
}

// BuildGroupMentionPrompt builds the prompt for @bot messages.
func BuildGroupMentionPrompt(groupName string, history []BufferedMessage, trigger BufferedMessage) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("以下是群「%s」的最近聊天记录：\n", groupName))
	sb.WriteString("---\n")
	for _, msg := range history {
		sb.WriteString(fmt.Sprintf("[%s] %s(%d): %s\n",
			msg.Time.Format(timeFormat), msg.Nickname, msg.UserID, msg.Text))
	}
	sb.WriteString("---\n\n")
	sb.WriteString("⬇️ 需要回复的消息：\n")
	sb.WriteString(fmt.Sprintf("[%s(%d)]: %s\n", trigger.Nickname, trigger.UserID, trigger.Text))

	return sb.String()
}

// BuildGroupActivePrompt builds the prompt for active (unsolicited) triggers.
func BuildGroupActivePrompt(groupName string, history []BufferedMessage) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("以下是群「%s」的最近聊天记录：\n", groupName))
	sb.WriteString("---\n")
	for _, msg := range history {
		sb.WriteString(fmt.Sprintf("[%s] %s(%d): %s\n",
			msg.Time.Format(timeFormat), msg.Nickname, msg.UserID, msg.Text))
	}
	sb.WriteString("---\n\n")
	sb.WriteString("你在旁边听到了这些对话，如果觉得有话想说可以自然地加入聊天，如果觉得没什么好说的就回复 [SKIP]。\n")

	return sb.String()
}

// BuildPrivatePrompt builds the prompt for private chat messages.
func BuildPrivatePrompt(nickname string, userID int64, text string) string {
	return fmt.Sprintf("[%s(%d)]: %s\n", nickname, userID, text)
}
