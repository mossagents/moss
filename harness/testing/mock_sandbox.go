package testing

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/workspace"
)

// MemorySandbox 是基于内存的 Workspace 测试桩。
type MemorySandbox struct {
	mu     sync.RWMutex
	Files  map[string][]byte
	Cmds   []ExecRecord
	limits workspace.ResourceLimits
}

// ExecRecord 记录一次命令执行。
type ExecRecord struct {
	Cmd  string
	Args []string
}

// NewMemorySandbox 创建内存 Workspace。
func NewMemorySandbox() *MemorySandbox {
	return &MemorySandbox{
		Files: make(map[string][]byte),
		limits: workspace.ResourceLimits{
			AllowedPaths: []string{"/workspace"},
		},
	}
}

func (s *MemorySandbox) ResolvePath(path string) (string, error) {
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path %q contains disallowed '..'", path)
	}
	return path, nil
}

func (s *MemorySandbox) ListFiles(_ context.Context, pattern string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []string
	for name := range s.Files {
		result = append(result, name)
	}
	return result, nil
}

func (s *MemorySandbox) ReadFile(_ context.Context, path string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.Files[path]
	if !ok {
		return nil, fmt.Errorf("file %q not found", path)
	}
	return data, nil
}

func (s *MemorySandbox) WriteFile(_ context.Context, path string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Files[path] = content
	return nil
}

func (s *MemorySandbox) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.Files[path]
	if !ok {
		return workspace.FileInfo{}, fmt.Errorf("file %q not found", path)
	}
	return workspace.FileInfo{Name: path, Size: int64(len(data))}, nil
}

func (s *MemorySandbox) DeleteFile(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Files, path)
	return nil
}

func (s *MemorySandbox) Execute(_ context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cmds = append(s.Cmds, ExecRecord{Cmd: req.Command, Args: req.Args})
	return workspace.ExecOutput{Stdout: "ok", ExitCode: 0}, nil
}

func (s *MemorySandbox) Capabilities() workspace.Capabilities {
	return workspace.Capabilities{
		FileSystemIsolation: workspace.IsolationMethodNone,
	}
}

func (s *MemorySandbox) Policy() workspace.SecurityPolicy {
	return workspace.SecurityPolicy{}
}

func (s *MemorySandbox) Limits() workspace.ResourceLimits {
	return s.limits
}
