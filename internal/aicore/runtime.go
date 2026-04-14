package aicore

import (
	"context"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"

	"github.com/colanns/gokohime/internal/config"
)

var (
	runtime     *api.Runtime
	runtimeOnce sync.Once
)

// InitRuntime creates and configures the agentsdk-go Runtime.
func InitRuntime(ctx context.Context, cfg *config.Config, customTools []tool.Tool, mw []middleware.Middleware) error {
	var initErr error
	runtimeOnce.Do(func() {
		var provider api.ModelFactory

		// Choose model provider based on config
		if cfg.AI.AnthropicAPIKey != "" {
			provider = &model.AnthropicProvider{
				ModelName: cfg.AI.Model,
			}
			log.Infof("[aicore] using Anthropic model: %s", cfg.AI.Model)
		} else if cfg.AI.OpenAIAPIKey != "" {
			provider = &model.OpenAIProvider{
				ModelName: cfg.AI.OpenAIModel,
				BaseURL:   cfg.AI.OpenAIAPIBase,
				APIKey:    cfg.AI.OpenAIAPIKey,
			}
			log.Infof("[aicore] using OpenAI-compatible model: %s", cfg.AI.OpenAIModel)
		} else {
			initErr = fmt.Errorf("no AI API key configured (set anthropic_api_key or openai_api_key)")
			return
		}

		opts := api.Options{
			ModelFactory:        provider,
			EnabledBuiltinTools: []string{}, // Disable all built-in tools (bash/read/write/etc.)
			CustomTools:         customTools,
			Middleware:          mw,
		}

		// Configure auto compact
		if cfg.AI.AutoCompact {
			opts.AutoCompact = api.CompactConfig{
				Enabled:       true,
				Threshold:     cfg.AI.CompactThreshold,
				PreserveCount: cfg.AI.CompactPreserveCount,
			}
		}

		runtime, initErr = api.New(ctx, opts)
		if initErr != nil {
			return
		}
		log.Info("[aicore] Agent runtime initialized")
	})
	return initErr
}

// GetRuntime returns the global Agent runtime.
func GetRuntime() *api.Runtime {
	return runtime
}

// Run executes a prompt synchronously.
func Run(ctx context.Context, prompt, sessionID string) (*api.Response, error) {
	if runtime == nil {
		return nil, fmt.Errorf("agent runtime not initialized")
	}
	log.Infof("[aicore] Run called session=%s prompt_len=%d", sessionID, len(prompt))
	resp, err := runtime.Run(ctx, api.Request{
		Prompt:    prompt,
		SessionID: sessionID,
	})
	if err != nil {
		log.Warnf("[aicore] Run failed session=%s err=%v", sessionID, err)
		return nil, err
	}
	if resp != nil && resp.Result != nil {
		log.Infof("[aicore] Run completed session=%s stop_reason=%s output_len=%d", sessionID, resp.Result.StopReason, len(resp.Result.Output))
	} else {
		log.Infof("[aicore] Run completed session=%s with empty response", sessionID)
	}
	return resp, nil
}

// RunStream executes a prompt and returns a streaming event channel.
func RunStream(ctx context.Context, prompt, sessionID string) (<-chan api.StreamEvent, error) {
	if runtime == nil {
		return nil, fmt.Errorf("agent runtime not initialized")
	}
	log.Infof("[aicore] RunStream called session=%s prompt_len=%d", sessionID, len(prompt))
	ch, err := runtime.RunStream(ctx, api.Request{
		Prompt:    prompt,
		SessionID: sessionID,
	})
	if err != nil {
		log.Warnf("[aicore] RunStream failed session=%s err=%v", sessionID, err)
		return nil, err
	}
	log.Infof("[aicore] RunStream started session=%s", sessionID)
	return ch, nil
}

// Close gracefully shuts down the runtime.
func Close() {
	if runtime != nil {
		runtime.Close()
		log.Info("[aicore] Agent runtime closed")
	}
}
