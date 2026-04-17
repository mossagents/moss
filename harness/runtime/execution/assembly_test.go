package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mossagents/moss/harness/runtime/execution"
	"github.com/mossagents/moss/kernel"
	kworkspace "github.com/mossagents/moss/kernel/workspace"
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
func (s *stubWorkspace) Execute(_ context.Context, _ kworkspace.ExecRequest) (kworkspace.ExecOutput, error) {
	return kworkspace.ExecOutput{}, nil
}
func (s *stubWorkspace) ResolvePath(_ string) (string, error)  { return "", nil }
func (s *stubWorkspace) Capabilities() kworkspace.Capabilities { return kworkspace.Capabilities{} }
func (s *stubWorkspace) Policy() kworkspace.SecurityPolicy     { return kworkspace.SecurityPolicy{} }
func (s *stubWorkspace) Limits() kworkspace.ResourceLimits     { return kworkspace.ResourceLimits{} }

// stubWorkspaceWithRoot is a workspace that reports a configurable root path.
type stubWorkspaceWithRoot struct {
	stubWorkspace
	root    string
	pathErr bool
}

func (s *stubWorkspaceWithRoot) ResolvePath(path string) (string, error) {
	if s.pathErr {
		return "", errors.New("resolve failed")
	}
	if path == "." {
		return s.root, nil
	}
	return s.root + "/" + path, nil
}
func (s *stubWorkspaceWithRoot) Capabilities() kworkspace.Capabilities {
	return kworkspace.Capabilities{}
}
func (s *stubWorkspaceWithRoot) Policy() kworkspace.SecurityPolicy {
	return kworkspace.SecurityPolicy{}
}
func (s *stubWorkspaceWithRoot) Limits() kworkspace.ResourceLimits {
	return kworkspace.ResourceLimits{}
}

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

func TestInstall_EmptyWorkspaceRoot(t *testing.T) {
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspace{}),
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
	)
	err := execution.Install(k, root, isolationRoot, true)
	if err != nil {
		t.Fatalf("unexpected error for valid isolation setup: %v", err)
	}
}

func TestInstall_WithMatchingWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspaceWithRoot{root: root}),
	)
	err := execution.Install(k, root, "", false)
	if err != nil {
		t.Fatalf("unexpected error when workspace root matches: %v", err)
	}
}

func TestInstall_WithMismatchedWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	differentRoot := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspaceWithRoot{root: differentRoot}),
	)
	err := execution.Install(k, root, "", false)
	if err == nil {
		t.Fatal("expected error when workspace root does not match")
	}
}

func TestInstall_WithWorkspacePathError(t *testing.T) {
	root := t.TempDir()
	k := kernel.New(
		kernel.WithWorkspace(&stubWorkspaceWithRoot{pathErr: true}),
	)
	// Workspace.ResolvePath errors → kernelWorkspaceRoot returns false → no mismatch check → success
	err := execution.Install(k, root, "", false)
	if err != nil {
		t.Fatalf("unexpected error when workspace ResolvePath fails (should skip check): %v", err)
	}
}
