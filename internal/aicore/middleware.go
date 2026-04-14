package aicore

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
)

// NewLoggingMiddleware creates a middleware that logs agent execution time.
func NewLoggingMiddleware() middleware.Middleware {
	return middleware.Funcs{
		Identifier: "logging",
		OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
			st.Values["start_time"] = time.Now()
			log.Debugf("[aicore] agent request started (session=%s)", st.Values["session_id"])
			return nil
		},
		OnAfterAgent: func(ctx context.Context, st *middleware.State) error {
			if start, ok := st.Values["start_time"].(time.Time); ok {
				log.Infof("[aicore] agent request completed in %v", time.Since(start))
			}
			return nil
		},
		OnBeforeTool: func(ctx context.Context, st *middleware.State) error {
			log.Debugf("[aicore] tool call: %v", st.Values["tool_name"])
			return nil
		},
		OnAfterTool: func(ctx context.Context, st *middleware.State) error {
			log.Debugf("[aicore] tool result: success=%v", st.Values["tool_success"])
			return nil
		},
	}
}
