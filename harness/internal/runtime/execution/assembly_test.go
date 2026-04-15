package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mossagents/moss/harness/internal/runtime/execution"
	"github.com/mossagents/moss/kernel"
	kworkspace "github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/harness/sandbox"
)

// stubWorkspace satisfies workspace.Workspace for testing.
type stubWorkspace struct{}

func (s *stubWorkspace) ReadFile(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (s *stubWorkspace) WriteFile(_ context.Context, _ string, _ []byte) error   { return nil }
func (s *stubWorkspace) ListFiles(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s *stubWorkspace) Stat(_ context.Context, _ string) (kworkspace.FileInfo, error) {
	return kworkspace.FileInfo{}, nil
}
func (s *stubWorkspace) DeleteFile(_ context.Context, _ string) error { return nil }

// stubSandbox satisfies sandbox.Sandbox for testing with a configurable root.
type stubSandbox struct {
	root    string
	pathErr bool
}

func (s *stubSandbox) ResolvePath(path string) (string, error) {
	if s.pathErr {
		return "", errors.New("resolve failed")
	}
	if path == "." {
		return s.root, nil
	}
	return s.root + "/" + path, nil
}
func (s *stubSandbox) ListFiles(_ string) ([]string, error) { return nil, nil }
func (s *stubSandbox) ReadFile(_ string) ([]byte, error)    { return nil, nil }
func (s *stubSandbox) WriteFile(_ string, _ []byte) error   { return nil }
func (s *stubSandbox) Execute(_ context.Context, _ kworkspace.ExecRequest) (kworkspace.ExecOutput, error) {
	return kworkspace.ExecOutput{}, nil
}
func (s *stubSandbox) Limits() sandbox.ResourceLimits { return sandbox.ResourceLimits{} }

func TestInstall_NilKernel(t *testing.T) {
	err := execution.Install(nil, "/tmp", "", false)
	if err == nil {
		t.Fatal("expected error for nil kernel")
	}
}

func TestInstall_NoWorkspace(t *testing.T) {
	k := kernel.New()
	err := execution.Install(k, t.TempDir(), "", false)
	if err == nil {
		t.Fatal("expected error when workspace is not set")
	}
}

func TestInstall_NoExecutor(t *testing.T) {
	k := kernel.New(kernel.WithWorkspace(&stubWorkspace{}))
	err := execution.Install(k, t.TempDir(), "", false)
	if err == nil {
		t.Fatal("expected error when executor is not set")
	}
}

func TestInstall_EmptyWorkspaceRoot(t *testing.T) {
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
	)
	err := execution.Install(k, "", "", false)
	if err == nil {
		t.Fatal("expected error for empty workspace root")
	}
}

func TestInstall_ValidSetup(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
	)
	err := execution.Install(k, root, "", false)
	if err != nil {
		t.Fatalf("unexpected error for valid setup: %v", err)
	}
}

func TestInstall_IsolationEnabled_EmptyIsolationRoot(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
	)
	err := execution.Install(k, root, "", true)
	if err == nil {
		t.Fatal("expected error when isolation enabled but isolation root is empty")
	}
}

func TestInstall_IsolationEnabled_ValidRoot(t *testing.T) {
	root := t.TempDir()
	isolationRoot := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
	)
	err := execution.Install(k, root, isolationRoot, true)
	if err != nil {
		t.Fatalf("unexpected error for valid isolation setup: %v", err)
	}
}

func TestInstall_WithMatchingSandboxRoot(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
		kernel.WithSandbox(&stubSandbox{root: root}),
	)
	err := execution.Install(k, root, "", false)
	if err != nil {
		t.Fatalf("unexpected error when sandbox root matches workspace root: %v", err)
	}
}

func TestInstall_WithMismatchedSandboxRoot(t *testing.T) {
	root := t.TempDir()
	differentRoot := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
		kernel.WithSandbox(&stubSandbox{root: differentRoot}),
	)
	err := execution.Install(k, root, "", false)
	if err == nil {
		t.Fatal("expected error when sandbox root does not match workspace root")
	}
}

func TestInstall_WithSandboxPathError(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
		kernel.WithSandbox(&stubSandbox{pathErr: true}),
	)
	// Sandbox.ResolvePath errors → kernelSandboxRoot returns false → no mismatch check → success
	err := execution.Install(k, root, "", false)
	if err != nil {
		t.Fatalf("unexpected error when sandbox ResolvePath fails (should skip check): %v", err)
	}
}
