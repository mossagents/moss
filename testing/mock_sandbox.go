package testing

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/sandbox"
)

// MemorySandbox 是基于内存的 Sandbox 测试桩。
type MemorySandbox struct {
	mu     sync.RWMutex
	Files  map[string][]byte
	Cmds   []ExecRecord
	limits sandbox.ResourceLimits
}

// ExecRecord 记录一次命令执行。
type ExecRecord struct {
	Cmd  string
	Args []string
}

// NewMemorySandbox 创建内存 Sandbox。
func NewMemorySandbox() *MemorySandbox {
	return &MemorySandbox{
		Files: make(map[string][]byte),
		limits: sandbox.ResourceLimits{
			AllowedPaths: []string{"/workspace"},
		},
	}
}

func (s *MemorySandbox) ResolvePath(path string) (string, error) {
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path %q contains disallowed ..", path)
	}
	return path, nil
}

func (s *MemorySandbox) ListFiles(pattern string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []string
	for name := range s.Files {
		result = append(result, name)
	}
	return result, nil
}

func (s *MemorySandbox) ReadFile(path string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.Files[path]
	if !ok {
		return nil, fmt.Errorf("file %q not found", path)
	}
	return data, nil
}

func (s *MemorySandbox) WriteFile(path string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Files[path] = content
	return nil
}

func (s *MemorySandbox) Execute(_ context.Context, req port.ExecRequest) (sandbox.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cmds = append(s.Cmds, ExecRecord{Cmd: req.Command, Args: req.Args})
	return sandbox.Output{Stdout: "ok", ExitCode: 0}, nil
}

func (s *MemorySandbox) Limits() sandbox.ResourceLimits {
	return s.limits
}
