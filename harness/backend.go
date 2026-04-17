package harness

import (
	"context"
	"fmt"
	"io"

	"github.com/mossagents/moss/harness/sandbox"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/workspace"
)

// LocalBackend composes a Workspace implementation with lifecycle hooks.
// It is the primary backend for local deployments.
type LocalBackend struct {
	workspace.Workspace
}

var _ workspace.Workspace = (*LocalBackend)(nil)

// OpenLocalBackend creates a LocalBackend backed by a local workspace rooted at root.
func OpenLocalBackend(root string, opts ...sandbox.Option) (*LocalBackend, error) {
	ws, err := sandbox.NewLocalWorkspace(root, opts...)
	if err != nil {
		return nil, fmt.Errorf("open local workspace: %w", err)
	}
	return &LocalBackend{Workspace: ws}, nil
}

// LocalBackendFactory creates LocalBackend instances for a given workspace root.
type LocalBackendFactory struct {
	Root    string
	Options []sandbox.Option
}

// NewLocalBackendFactory returns a factory that provisions local backends.
func NewLocalBackendFactory(root string, opts ...sandbox.Option) LocalBackendFactory {
	return LocalBackendFactory{
		Root:    root,
		Options: append([]sandbox.Option(nil), opts...),
	}
}

func (f LocalBackendFactory) Build(_ context.Context, k *kernel.Kernel) (workspace.Workspace, error) {
	if k != nil && k.Workspace() != nil {
		return &LocalBackend{Workspace: k.Workspace()}, nil
	}
	ws, err := sandbox.NewLocalWorkspace(f.Root, f.Options...)
	if err != nil {
		return nil, fmt.Errorf("open local workspace: %w", err)
	}
	return &LocalBackend{Workspace: ws}, nil
}

// BackendFactory builds a workspace for a specific Kernel assembly.
type BackendFactory interface {
	Build(context.Context, *kernel.Kernel) (workspace.Workspace, error)
}

// BackendFactoryFunc adapts a function into a BackendFactory.
type BackendFactoryFunc func(context.Context, *kernel.Kernel) (workspace.Workspace, error)

func (f BackendFactoryFunc) Build(ctx context.Context, k *kernel.Kernel) (workspace.Workspace, error) {
	return f(ctx, k)
}

// BackendInstaller configures kernel ports from a backend before feature installation.
type BackendInstaller interface {
	Install(context.Context, *kernel.Kernel) error
}

// BackendBooter participates in kernel boot after the backend is activated.
type BackendBooter interface {
	Boot(context.Context, *kernel.Kernel) error
}

// BackendShutdowner participates in kernel shutdown after feature-owned resources finish.
type BackendShutdowner interface {
	Shutdown(context.Context, *kernel.Kernel) error
}

// Install wires the local backend into the Kernel.
func (b *LocalBackend) Install(_ context.Context, k *kernel.Kernel) error {
	if b == nil {
		return fmt.Errorf("local backend is nil")
	}
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	if k.Workspace() == nil && b.Workspace != nil {
		k.Apply(kernel.WithWorkspace(b.Workspace))
	}
	if b.Workspace == nil {
		b.Workspace = k.Workspace()
	}
	if b.Workspace == nil {
		return fmt.Errorf("backend did not provide workspace port")
	}
	return nil
}

// Shutdown closes the underlying workspace if it exposes io.Closer.
func (b *LocalBackend) Shutdown(_ context.Context, _ *kernel.Kernel) error {
	if b == nil || b.Workspace == nil {
		return nil
	}
	closer, ok := b.Workspace.(io.Closer)
	if !ok || closer == nil {
		return nil
	}
	return closer.Close()
}
