package omoi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
)

const kvNamespace = "omoi"

// getGroupSession returns the Omoi session ID for a group, creating one if needed.
func getGroupSession(ctx context.Context, groupID int64, groupName string) (string, error) {
	key := "session:group:" + strconv.FormatInt(groupID, 10)
	return getOrCreateSession(ctx, key, "群:"+groupName)
}

// getPrivateSession returns the Omoi session ID for a private chat, creating one if needed.
func getPrivateSession(ctx context.Context, userID int64, nickname string) (string, error) {
	key := "session:private:" + strconv.FormatInt(userID, 10)
	return getOrCreateSession(ctx, key, "私聊:"+nickname)
}

// getOrCreateSession looks up a session from KV, creating one if absent.
func getOrCreateSession(ctx context.Context, key, title string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("omoi client not initialized")
	}

	sessionID, err := database.GetPluginKV(ctx, nil, kvNamespace, key)
	if err == nil && sessionID != "" {
		return sessionID, nil
	}

	// Create new session
	sessionID, err = client.CreateSession(ctx, title)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	if err := database.SetPluginKV(ctx, nil, kvNamespace, key, sessionID); err != nil {
		log.Warnf("[omoi] failed to persist session to KV: %v", err)
	}

	log.Infof("[omoi] created session %s for %s", sessionID, key)
	return sessionID, nil
}

// resetSession deletes a session mapping from KV so the next call creates a fresh one.
func resetSession(ctx context.Context, key string) error {
	if client != nil {
		session, err := database.GetPluginKV(ctx, nil, kvNamespace, key)
		if err == nil && session != "" {
			err = client.deliveryAPI(ctx, "DELETE", "/sessions/"+session+"/delivery", nil, nil)
			if err != nil && !strings.Contains(err.Error(), "404") {
				return err
			}
		}
	}
	return database.SetPluginKV(ctx, nil, kvNamespace, key, "")
}

// resetGroupSession resets the group session.
func resetGroupSession(ctx context.Context, groupID int64) error {
	key := "session:group:" + strconv.FormatInt(groupID, 10)
	return resetSession(ctx, key)
}

// resetPrivateSession resets the private session.
func resetPrivateSession(ctx context.Context, userID int64) error {
	key := "session:private:" + strconv.FormatInt(userID, 10)
	return resetSession(ctx, key)
}

// sendGroupMessage shares session recovery between mentions and active chat.
func sendGroupMessage(ctx context.Context, groupID int64, groupName, prompt string, refs ...MediaReference) (string, error) {
	sessionID, err := getGroupSession(ctx, groupID, groupName)
	if err != nil {
		return "", err
	}
	reply, err := client.SendMessage(ctx, sessionID, prompt, refs...)
	if err == nil || !strings.Contains(err.Error(), "404") {
		return reply, err
	}
	if err := resetGroupSession(ctx, groupID); err != nil {
		return "", err
	}
	sessionID, err = getGroupSession(ctx, groupID, groupName)
	if err != nil {
		return "", err
	}
	return client.SendMessage(ctx, sessionID, prompt, refs...)
}
