package defaults

import (
	"context"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
)

// Option is kept for backward compatibility and forwarded to runtime.Option.
type Option = runtime.Option

// Deprecated: use runtime.WithBuiltinTools(false).
func WithoutBuiltin() Option {
	return runtime.WithBuiltinTools(false)
}

// Deprecated: use runtime.WithMCPServers(false).
func WithoutMCPServers() Option {
	return runtime.WithMCPServers(false)
}

// Deprecated: use runtime.WithSkills(false).
func WithoutSkills() Option {
	return runtime.WithSkills(false)
}

// Deprecated: use runtime.WithProgressiveSkills(true).
func WithProgressiveSkills() Option {
	return runtime.WithProgressiveSkills(true)
}

// Setup is a compatibility shim forwarding to appkit/runtime.Setup.
func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	return runtime.Setup(ctx, k, workspaceDir, opts...)
}
