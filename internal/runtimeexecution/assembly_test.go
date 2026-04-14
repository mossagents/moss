package runtimeexecution_test

import (
	"context"
	"testing"

	"github.com/mossagents/moss/internal/runtimeexecution"
	"github.com/mossagents/moss/kernel"
	kworkspace "github.com/mossagents/moss/kernel/workspace"
)

// stubWorkspace satisfies workspace.Workspace for testing.
type stubWorkspace struct{}

func (s *stubWorkspace) ReadFile(_ context.Context, _ string) ([]byte, error)           { return nil, nil }
func (s *stubWorkspace) WriteFile(_ context.Context, _ string, _ []byte) error          { return nil }
func (s *stubWorkspace) ListFiles(_ context.Context, _ string) ([]string, error)        { return nil, nil }
func (s *stubWorkspace) Stat(_ context.Context, _ string) (kworkspace.FileInfo, error)  { return kworkspace.FileInfo{}, nil }
func (s *stubWorkspace) DeleteFile(_ context.Context, _ string) error                   { return nil }

func TestInstall_NilKernel(t *testing.T) {
	err := runtimeexecution.Install(nil, "/tmp", "", false)
	if err == nil {
		t.Fatal("expected error for nil kernel")
	}
}

func TestInstall_NoWorkspace(t *testing.T) {
	k := kernel.New()
	err := runtimeexecution.Install(k, t.TempDir(), "", false)
	if err == nil {
		t.Fatal("expected error when workspace is not set")
	}
}

func TestInstall_NoExecutor(t *testing.T) {
	k := kernel.New(kernel.WithWorkspace(&stubWorkspace{}))
	err := runtimeexecution.Install(k, t.TempDir(), "", false)
	if err == nil {
		t.Fatal("expected error when executor is not set")
	}
}

func TestInstall_EmptyWorkspaceRoot(t *testing.T) {
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
		kernel.WithExecutor(kworkspace.NoOpExecutor{}),
	)
	err := runtimeexecution.Install(k, "", "", false)
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
	err := runtimeexecution.Install(k, root, "", false)
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
	err := runtimeexecution.Install(k, root, "", true)
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
	err := runtimeexecution.Install(k, root, isolationRoot, true)
	if err != nil {
		t.Fatalf("unexpected error for valid isolation setup: %v", err)
	}
}
