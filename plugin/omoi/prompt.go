package omoi

import (
	"fmt"
	"strings"
)

const timeFormat = "2006-01-02 15:04"

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
