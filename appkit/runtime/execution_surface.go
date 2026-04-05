package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/sandbox"
)

type executionSurface struct {
	sandbox   sandbox.Sandbox
	workspace port.Workspace
	executor  port.Executor
}

func newExecutionSurface(sb sandbox.Sandbox, ws port.Workspace, exec port.Executor) executionSurface {
	surface := executionSurface{
		sandbox:   sb,
		workspace: ws,
		executor:  exec,
	}
	if surface.workspace == nil && sb != nil {
		surface.workspace = sandboxWorkspacePort{sb: sb}
	}
	if surface.executor == nil && sb != nil {
		surface.executor = sandboxExecutorPort{sb: sb}
	}
	return surface
}

func (s executionSurface) HasWorkspace() bool { return s.workspace != nil }
func (s executionSurface) HasExecutor() bool  { return s.executor != nil }

func (s executionSurface) Workspace() port.Workspace { return s.workspace }
func (s executionSurface) Executor() port.Executor   { return s.executor }
func (s executionSurface) Sandbox() sandbox.Sandbox  { return s.sandbox }

func (s executionSurface) ResolveRoot() string {
	if s.sandbox == nil {
		return ""
	}
	root, err := s.sandbox.ResolvePath(".")
	if err != nil {
		return ""
	}
	return root
}

func (s executionSurface) WriteBytes(ctx context.Context, path string, data []byte) error {
	if s.workspace == nil {
		return fmt.Errorf("workspace is unavailable")
	}
	return s.workspace.WriteFile(ctx, path, data)
}

type sandboxWorkspacePort struct {
	sb sandbox.Sandbox
}

func (s sandboxWorkspacePort) ReadFile(_ context.Context, path string) ([]byte, error) {
	return s.sb.ReadFile(path)
}

func (s sandboxWorkspacePort) WriteFile(_ context.Context, path string, data []byte) error {
	return s.sb.WriteFile(path, data)
}

func (s sandboxWorkspacePort) ListFiles(_ context.Context, pattern string) ([]string, error) {
	return s.sb.ListFiles(pattern)
}

func (s sandboxWorkspacePort) Stat(_ context.Context, path string) (port.FileInfo, error) {
	resolved, err := s.sb.ResolvePath(path)
	if err != nil {
		return port.FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return port.FileInfo{}, err
	}
	return port.FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

func (s sandboxWorkspacePort) DeleteFile(_ context.Context, path string) error {
	resolved, err := s.sb.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

type sandboxExecutorPort struct {
	sb sandbox.Sandbox
}

func (s sandboxExecutorPort) Execute(ctx context.Context, req port.ExecRequest) (port.ExecOutput, error) {
	return s.sb.Execute(ctx, req)
}
