package gatewayx

import (
	"context"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// ServeConfig is retained for compatibility and forwarded to runtime.ServeConfig.
type ServeConfig struct {
	Prompt       string
	SystemPrompt string
	SessionStore session.SessionStore
	RouterConfig session.RouterConfig
	OnError      func(error)
}

// Deprecated: use runtime.ServeCLI.
func ServeCLI(ctx context.Context, cfg ServeConfig, k *kernel.Kernel) error {
	return runtime.ServeCLI(ctx, runtime.ServeConfig{
		Prompt:       cfg.Prompt,
		SystemPrompt: cfg.SystemPrompt,
		SessionStore: cfg.SessionStore,
		RouterConfig: cfg.RouterConfig,
		OnError:      cfg.OnError,
	}, k)
}
