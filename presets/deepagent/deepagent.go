package deepagent

import (
	"context"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
)

// Config reuses appkit deep preset configuration for compatibility.
type Config = appkit.DeepAgentConfig

// DefaultConfig returns the default deep preset configuration.
func DefaultConfig() Config {
	return appkit.DefaultDeepAgentConfig()
}

// BuildKernel builds a deep-agent style kernel preset.
func BuildKernel(ctx context.Context, flags *appkit.AppFlags, io port.UserIO, cfg *Config) (*kernel.Kernel, error) {
	return appkit.BuildDeepAgentKernel(ctx, flags, io, cfg)
}
