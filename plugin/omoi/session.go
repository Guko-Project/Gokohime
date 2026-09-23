package omoi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
)

const kvNamespace = "omoi"

var sessionLocks sync.Map

func sessionLock(key string) *sync.Mutex {
	lock, _ := sessionLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func sessionKey(ctx context.Context, key string) string {
	if memory, ok := memoryFrom(ctx); ok && client != nil {
		return key + ":memory:" + client.agentID + ":" + memory.Source.ConnectorID
	}
	return key
}

// getGroupSession returns the Omoi session ID for a group, creating one if needed.
// created reports whether a fresh session was just created and needs seeding.
func getGroupSession(ctx context.Context, groupID int64, groupName string) (sessionID string, created bool, err error) {
	key := "session:group:" + strconv.FormatInt(groupID, 10)
	return getOrCreateSession(ctx, key, "群:"+groupName)
}

// getPrivateSession returns the Omoi session ID for a private chat, creating one if needed.
func getPrivateSession(ctx context.Context, userID int64, nickname string) (string, error) {
	key := "session:private:" + strconv.FormatInt(userID, 10)
	sessionID, _, err := getOrCreateSession(ctx, key, "私聊:"+nickname)
	return sessionID, err
}

// getOrCreateSession looks up a session from KV, creating one if absent.
func getOrCreateSession(ctx context.Context, key, title string) (string, bool, error) {
	return getOrCreateSessionAfterFailure(ctx, key, title, "")
}

func getOrCreateSessionAfterFailure(ctx context.Context, key, title, failedID string) (string, bool, error) {
	if client == nil {
		return "", false, fmt.Errorf("omoi client not initialized")
	}
	key = sessionKey(ctx, key)
	lock := sessionLock(key)
	lock.Lock()
	defer lock.Unlock()

	sessionID, err := database.GetPluginKV(ctx, nil, kvNamespace, key)
	if err == nil && sessionID != "" && sessionID != failedID {
		return sessionID, false, nil
	}

	// Create new session
	sessionID, err = client.CreateSession(ctx, title)
	if err != nil {
		return "", false, fmt.Errorf("create session: %w", err)
	}

	if err := database.SetPluginKV(ctx, nil, kvNamespace, key, sessionID); err != nil {
		return "", false, fmt.Errorf("persist session: %w", err)
	}

	log.Infof("[omoi] created session %s for %s", sessionID, key)
	return sessionID, true, nil
}

// resetSession deletes a session mapping from KV so the next call creates a fresh one.
func resetSession(ctx context.Context, key string) error {
	keys := []string{key}
	connector := ""
	if memory, ok := memoryFrom(ctx); ok {
		connector = memory.Source.ConnectorID
	} else if channel, ok := ctx.Value(channelKey{}).(channelContext); ok {
		connector = strings.TrimPrefix(channel.Instance, "qq:")
	}
	if client != nil && connector != "" {
		keys = append(keys, key+":memory:"+client.agentID+":"+connector)
	}
	for _, key := range keys {
		if err := resetSessionKey(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func resetSessionKey(ctx context.Context, key string) error {
	lock := sessionLock(key)
	lock.Lock()
	defer lock.Unlock()
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

// sendGroupMessage delivers each buffered group message to the session exactly
// once. A fresh session receives the whole buffer as a seed; later requests
// carry only messages the session has not seen. sendMu keeps the
// pending→send→markSent sequence atomic against concurrent triggers.
func sendGroupMessage(ctx context.Context, buf *GroupBuffer, groupID int64, groupName string, trigger *BufferedMessage, refs ...MediaReference) (string, error) {
	buf.sendMu.Lock()
	defer buf.sendMu.Unlock()

	key := "session:group:" + strconv.FormatInt(groupID, 10)
	sessionID, created, err := getOrCreateSession(ctx, key, "群:"+groupName)
	if err != nil {
		return "", err
	}
	reply, err := sendGroupPrompt(ctx, buf, sessionID, groupName, trigger, created, refs)
	if err == nil || !strings.Contains(err.Error(), "404") {
		return reply, err
	}
	sessionID, _, err = getOrCreateSessionAfterFailure(ctx, key, "群:"+groupName, sessionID)
	if err != nil {
		return "", err
	}
	return sendGroupPrompt(ctx, buf, sessionID, groupName, trigger, true, refs)
}

func sendGroupPrompt(ctx context.Context, buf *GroupBuffer, sessionID, groupName string, trigger *BufferedMessage, seed bool, refs []MediaReference) (string, error) {
	msgs, omitted := buf.pending(seed)
	if len(msgs) == 0 {
		return "", fmt.Errorf("no new group messages to send")
	}
	var prompt string
	if seed {
		if trigger != nil {
			var history []BufferedMessage
			for _, m := range msgs {
				if m.seq != trigger.seq {
					history = append(history, m)
				}
			}
			prompt = BuildGroupMentionPrompt(groupName, history, *trigger)
		} else {
			prompt = BuildGroupActivePrompt(groupName, msgs, config.Get().Omoi.SkipMarker)
		}
	} else {
		prompt = BuildGroupDeltaPrompt(groupName, msgs, omitted, trigger, config.Get().Omoi.SkipMarker)
	}
	reply, err := client.SendMessage(ctx, sessionID, prompt, selectMedia(refs, msgs)...)
	if err != nil {
		return "", err
	}
	buf.markSent(msgs[len(msgs)-1].seq)
	return reply, nil
}
