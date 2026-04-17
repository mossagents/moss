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
	kernel       *kernel.Kernel
	backend      Backend
	backendReady bool
	features     []Feature
}

// New creates a Harness around an existing Kernel and Backend.
func New(k *kernel.Kernel, backend Backend) *Harness {
	return &Harness{
		kernel:  k,
		backend: backend,
	}
}

const (
	backendBootOrder     = -100
	backendShutdownOrder = 1000
)

// NewWithBackendFactory builds a backend via factory, activates it against the
// Kernel, and returns a ready Harness.
func NewWithBackendFactory(ctx context.Context, k *kernel.Kernel, factory BackendFactory) (*Harness, error) {
	if factory == nil {
		return nil, fmt.Errorf("backend factory is nil")
	}
	backend, err := factory.Build(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("build backend: %w", err)
	}
	if backend == nil {
		return nil, fmt.Errorf("backend factory returned nil backend")
	}
	h := New(k, backend)
	if err := h.ActivateBackend(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

// Kernel returns the underlying Kernel.
func (h *Harness) Kernel() *kernel.Kernel { return h.kernel }

// Backend returns the underlying Backend.
func (h *Harness) Backend() Backend { return h.backend }

// ActivateBackend wires any managed backend hooks and kernel ports exactly once.
func (h *Harness) ActivateBackend(ctx context.Context) error {
	if h == nil {
		return fmt.Errorf("harness is nil")
	}
	return h.ensureBackend(ctx)
}

// Install applies Features to the Harness in order. Each feature may
// register tools, hooks, system-prompt extensions, or kernel options.
// Official features are installed under phase/dependency governance.
// If any feature fails, already-installed features that implement Uninstaller
// are rolled back in reverse order.
func (h *Harness) Install(ctx context.Context, features ...Feature) error {
	if err := h.ensureBackend(ctx); err != nil {
		return err
	}
	planned, err := h.planFeatures(features...)
	if err != nil {
		return err
	}
	var installed []Feature
	for _, item := range planned {
		if err := item.feature.Install(ctx, h); err != nil {
			for i := len(installed) - 1; i >= 0; i-- {
				if u, ok := installed[i].(Uninstaller); ok {
					_ = u.Uninstall(ctx, h)
				}
			}
			return fmt.Errorf("feature %q: %w", item.feature.Name(), err)
		}
		installed = append(installed, item.feature)
	}
	h.features = append(h.features, installed...)
	return nil
}

// InstalledFeatures returns the list of successfully installed features.
func (h *Harness) InstalledFeatures() []Feature { return append([]Feature(nil), h.features...) }

func (h *Harness) ensureBackend(ctx context.Context) error {
	if h.backendReady || h.backend == nil {
		return nil
	}
	if installer, ok := h.backend.(BackendInstaller); ok {
		if err := installer.Install(ctx, h.kernel); err != nil {
			return fmt.Errorf("activate backend: %w", err)
		}
	}
	stages := h.kernel.Stages()
	if booter, ok := h.backend.(BackendBooter); ok {
		if err := stages.OnBoot(backendBootOrder, func(ctx context.Context, k *kernel.Kernel) error {
			return booter.Boot(ctx, k)
		}); err != nil {
			return fmt.Errorf("register backend boot hook: %w", err)
		}
	}
	if shutdowner, ok := h.backend.(BackendShutdowner); ok {
		if err := stages.OnShutdown(backendShutdownOrder, func(ctx context.Context, k *kernel.Kernel) error {
			return shutdowner.Shutdown(ctx, k)
		}); err != nil {
			return fmt.Errorf("register backend shutdown hook: %w", err)
		}
	}
	h.backendReady = true
	return nil
}
