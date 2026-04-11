// Package harness provides a composable orchestration layer between the
// Moss Kernel and applications. It introduces the Feature interface for
// pluggable capabilities (tools, hooks, system-prompt extensions) and the
// Backend interface that unifies workspace and command-execution ports.
//
// Usage:
//
//	k := kernel.New(kernel.WithLLM(llm), kernel.WithUserIO(io))
//	backend := &harness.LocalBackend{Workspace: ws, Executor: exec}
//	h := harness.New(k, backend)
//	err := h.Install(ctx,
//	    harness.BootstrapContext(workspace, appName, trust),
//	    harness.LLMResilience(retryConfig, nil),
//	    harness.Checkpointing(store),
//	)
package harness

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel"
)

// Harness orchestrates a Kernel with a Backend and composable Features.
type Harness struct {
	kernel   *kernel.Kernel
	backend  Backend
	features []Feature
}

// New creates a Harness around an existing Kernel and Backend.
func New(k *kernel.Kernel, backend Backend) *Harness {
	return &Harness{
		kernel:  k,
		backend: backend,
	}
}

// Kernel returns the underlying Kernel.
func (h *Harness) Kernel() *kernel.Kernel { return h.kernel }

// Backend returns the underlying Backend.
func (h *Harness) Backend() Backend { return h.backend }

// Install applies Features to the Harness in order. Each feature may
// register tools, hooks, system-prompt extensions, or kernel options.
func (h *Harness) Install(ctx context.Context, features ...Feature) error {
	for _, f := range features {
		if f == nil {
			continue
		}
		if err := f.Install(ctx, h); err != nil {
			return fmt.Errorf("feature %q: %w", f.Name(), err)
		}
		h.features = append(h.features, f)
	}
	return nil
}

// InstalledFeatures returns the list of successfully installed features.
func (h *Harness) InstalledFeatures() []Feature { return h.features }
