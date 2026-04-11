// Package deepagent provides a deep-agent style kernel preset.
//
// Deprecated: Use harness/patterns.BuildDeepAgent directly.
// This package is a thin wrapper for backward compatibility.
package deepagent

import (
	"context"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/harness/patterns"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
)

// Config is an alias for patterns.DeepAgentConfig.
type Config = patterns.DeepAgentConfig

// DefaultConfig returns the default deep-agent preset configuration.
func DefaultConfig() Config {
	return patterns.DeepAgentDefaults()
}

// BuildKernel builds a deep-agent style kernel preset.
//
// Deprecated: Use patterns.BuildDeepAgent directly.
func BuildKernel(ctx context.Context, flags *appkit.AppFlags, uio io.UserIO, cfg *Config) (*kernel.Kernel, error) {
	return patterns.BuildDeepAgent(ctx, flags, uio, cfg)
}
