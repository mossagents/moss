package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LocalSandbox 是基于本地文件系统的 Sandbox 实现。
type LocalSandbox struct {
	root   string
	limits ResourceLimits
}

// NewLocal 在指定根目录创建 LocalSandbox。
func NewLocal(root string, opts ...LocalOption) (*LocalSandbox, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve sandbox root: %w", err)
	}
	s := &LocalSandbox{
		root: absRoot,
		limits: ResourceLimits{
			MaxFileSize:    10 * 1024 * 1024, // 10 MB
			CommandTimeout: 30 * time.Second,
			AllowedPaths:   []string{absRoot},
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// LocalOption 配置 LocalSandbox。
type LocalOption func(*LocalSandbox)

// WithMaxFileSize 设置文件大小限制。
func WithMaxFileSize(n int64) LocalOption {
	return func(s *LocalSandbox) { s.limits.MaxFileSize = n }
}

// WithCommandTimeout 设置命令执行超时。
func WithCommandTimeout(d time.Duration) LocalOption {
	return func(s *LocalSandbox) { s.limits.CommandTimeout = d }
}

// WithAllowedPaths 设置额外的路径白名单。
func WithAllowedPaths(paths ...string) LocalOption {
	return func(s *LocalSandbox) {
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err == nil {
				s.limits.AllowedPaths = append(s.limits.AllowedPaths, abs)
			}
		}
	}
}

func (s *LocalSandbox) ResolvePath(path string) (string, error) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(s.root, path))
	}

	for _, allowed := range s.limits.AllowedPaths {
		if strings.HasPrefix(abs, allowed+string(filepath.Separator)) || abs == allowed {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %q escapes sandbox (root=%s)", path, s.root)
}

func (s *LocalSandbox) ListFiles(pattern string) ([]string, error) {
	fullPattern := filepath.Join(s.root, pattern)
	return filepath.Glob(fullPattern)
}

func (s *LocalSandbox) ReadFile(path string) ([]byte, error) {
	resolved, err := s.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

func (s *LocalSandbox) WriteFile(path string, content []byte) error {
	resolved, err := s.ResolvePath(path)
	if err != nil {
		return err
	}
	if s.limits.MaxFileSize > 0 && int64(len(content)) > s.limits.MaxFileSize {
		return fmt.Errorf("file size %d exceeds limit %d", len(content), s.limits.MaxFileSize)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}
	return os.WriteFile(resolved, content, 0644)
}

func (s *LocalSandbox) Execute(ctx context.Context, cmd string, args []string) (Output, error) {
	timeout := s.limits.CommandTimeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = s.root

	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	out := Output{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			out.ExitCode = exitErr.ExitCode()
		} else {
			return out, err
		}
	}
	return out, nil
}

func (s *LocalSandbox) Limits() ResourceLimits {
	return s.limits
}
