package harness

import (
	"context"
	"fmt"
	"io"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/harness/sandbox"
)

// Backend provides unified file-system and command-execution capabilities
// for agent operations. It combines workspace.Workspace (file I/O) with
// workspace.Executor (command execution) into a single deployment unit.
//
// Different deployment scenarios (local, Docker, remote, cloud) provide
// their own Backend implementation.
type Backend interface {
	workspace.Workspace
	workspace.Executor
}

// BackendFactory builds a Backend for a specific Kernel assembly.
type BackendFactory interface {
	Build(context.Context, *kernel.Kernel) (Backend, error)
}

// BackendFactoryFunc adapts a function into a BackendFactory.
type BackendFactoryFunc func(context.Context, *kernel.Kernel) (Backend, error)

func (f BackendFactoryFunc) Build(ctx context.Context, k *kernel.Kernel) (Backend, error) {
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

// LocalBackend composes an existing Workspace and Executor into a Backend.
// When Sandbox is provided, Install wires it into the Kernel and adopts the
// effective Workspace/Executor ports selected by the Kernel.
type LocalBackend struct {
	workspace.Workspace
	workspace.Executor
	Sandbox sandbox.Sandbox
}

var _ Backend = (*LocalBackend)(nil)

// OpenLocalBackend creates a LocalBackend backed by a local sandbox rooted at root.
func OpenLocalBackend(root string, opts ...sandbox.LocalOption) (*LocalBackend, error) {
	sb, err := sandbox.NewLocal(root, opts...)
	if err != nil {
		return nil, fmt.Errorf("open local sandbox: %w", err)
	}
	return &LocalBackend{Sandbox: sb}, nil
}

// LocalBackendFactory creates LocalBackend instances for a given workspace root.
type LocalBackendFactory struct {
	Root    string
	Options []sandbox.LocalOption
}

// NewLocalBackendFactory returns a factory that provisions local backends.
func NewLocalBackendFactory(root string, opts ...sandbox.LocalOption) LocalBackendFactory {
	return LocalBackendFactory{
		Root:    root,
		Options: append([]sandbox.LocalOption(nil), opts...),
	}
}

func (f LocalBackendFactory) Build(_ context.Context, k *kernel.Kernel) (Backend, error) {
	backend := &LocalBackend{}
	if k != nil {
		backend.Workspace = k.Workspace()
		backend.Executor = k.Executor()
		backend.Sandbox = k.Sandbox()
	}
	if backend.Sandbox == nil && (backend.Workspace == nil || backend.Executor == nil) {
		sb, err := sandbox.NewLocal(f.Root, f.Options...)
		if err != nil {
			return nil, fmt.Errorf("open local sandbox: %w", err)
		}
		backend.Sandbox = sb
	}
	return backend, nil
}

// Install wires the local backend into the Kernel without overwriting ports
// that were already injected by the caller.
func (b *LocalBackend) Install(_ context.Context, k *kernel.Kernel) error {
	if b == nil {
		return fmt.Errorf("local backend is nil")
	}
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	var opts []kernel.Option
	if k.Sandbox() == nil && b.Sandbox != nil {
		opts = append(opts, kernel.WithSandbox(b.Sandbox))
	}
	if k.Workspace() == nil && b.Workspace != nil {
		opts = append(opts, kernel.WithWorkspace(b.Workspace))
	}
	if k.Executor() == nil && b.Executor != nil {
		opts = append(opts, kernel.WithExecutor(b.Executor))
	}
	if len(opts) > 0 {
		k.Apply(opts...)
	}
	if b.Workspace == nil {
		b.Workspace = k.Workspace()
	}
	if b.Executor == nil {
		b.Executor = k.Executor()
	}
	if b.Workspace == nil || b.Executor == nil {
		return fmt.Errorf("backend did not provide workspace/executor ports")
	}
	return nil
}

// Shutdown closes the underlying sandbox if it exposes io.Closer.
func (b *LocalBackend) Shutdown(_ context.Context, _ *kernel.Kernel) error {
	if b == nil || b.Sandbox == nil {
		return nil
	}
	closer, ok := b.Sandbox.(io.Closer)
	if !ok || closer == nil {
		return nil
	}
	return closer.Close()
}
