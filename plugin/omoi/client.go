package omoi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/config"
	log "github.com/colanns/gokohime/internal/log"
)

// OmoiClient wraps Omoi HTTP/SSE API calls.
type OmoiClient struct {
	baseURL    string
	apiKey     string
	agentID    string
	httpClient *http.Client
}

var client *OmoiClient

func initClient() {
	cfg := config.Get().Omoi
	if cfg.Address == "" || cfg.APIKey == "" || cfg.AgentID == "" {
		log.Warn("[omoi] disabled: address, api_key, or agent_id not configured")
		return
	}
	client = &OmoiClient{
		baseURL: strings.TrimRight(cfg.Address, "/"),
		apiKey:  cfg.APIKey,
		agentID: cfg.AgentID,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}
	log.Infof("[omoi] client initialized: %s agent=%s", client.baseURL, client.agentID)
}

// CreateSession creates a new Omoi session and returns its ID.
func (c *OmoiClient) CreateSession(ctx context.Context, title string) (string, error) {
	body := map[string]string{
		"agent_id": c.agentID,
		"title":    title,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sessions", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.ID, nil
}

// SendMessage sends a message to an Omoi session and returns the full assistant reply.
// It consumes the SSE stream, concatenating delta events.
func (c *OmoiClient) SendMessage(ctx context.Context, sessionID, text string) (string, error) {
	body := map[string]any{
		"session_id": sessionID,
		"content":    []map[string]string{{"type": "text", "text": text}},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	// Use a separate client without global timeout for streaming.
	streamClient := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/messages", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := streamClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("send message: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse SSE stream
	return c.consumeSSE(resp.Body)
}

// consumeSSE reads SSE events from the response body and accumulates the assistant reply.
func (c *OmoiClient) consumeSSE(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	var reply strings.Builder
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			currentEvent = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "delta":
				var delta struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal([]byte(dataStr), &delta); err == nil {
					reply.WriteString(delta.Content)
				}

			case "error":
				var errData struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal([]byte(dataStr), &errData); err == nil {
					return reply.String(), fmt.Errorf("omoi error: %s", errData.Message)
				}
				return reply.String(), fmt.Errorf("omoi error: %s", dataStr)

			case "done":
				return reply.String(), nil

			case "tool_start", "tool_end":
				// Ignore tool events
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return reply.String(), fmt.Errorf("read SSE: %w", err)
	}

	// Stream ended without a done event
	return reply.String(), nil
}
