package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mossagi/moss/kernel/port"
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
	// 支持 ** 递归匹配
	if strings.Contains(pattern, "**") {
		return s.listFilesRecursive(pattern)
	}
	fullPattern := filepath.Join(s.root, pattern)
	return filepath.Glob(fullPattern)
}

// listFilesRecursive 用 WalkDir 实现 ** 递归 glob 匹配。
func (s *LocalSandbox) listFilesRecursive(pattern string) ([]string, error) {
	// 将 pattern 拆为前缀（** 之前）和后缀（** 之后）
	// 例如 "**/*.go" → prefix="", suffix="*.go"
	// 例如 "src/**/*.go" → prefix="src", suffix="*.go"
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/\\")
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimLeft(parts[1], "/\\")
	}

	searchRoot := s.root
	if prefix != "" {
		searchRoot = filepath.Join(s.root, prefix)
	}

	var matches []string
	err := filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无权限等错误
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if suffix == "" {
			matches = append(matches, path)
			return nil
		}
		ok, _ := filepath.Match(suffix, name)
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
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

	// 如果无参数且命令包含 shell 特殊字符，自动用 shell 包装。
	// 这样 LLM 可以直接发送 "ls -la" 或 "go build ./..." 等完整 shell 命令。
	if len(args) == 0 && needsShell(cmd) {
		if runtime.GOOS == "windows" {
			args = []string{"/C", cmd}
			cmd = "cmd"
		} else {
			args = []string{"-c", cmd}
			cmd = "sh"
		}
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

// needsShell 检查命令是否需要 shell 包装（包含空格、管道、重定向等）。
func needsShell(cmd string) bool {
	return strings.ContainsAny(cmd, " \t|><&;$`")
}

func (s *LocalSandbox) Limits() ResourceLimits {
	return s.limits
}

// LocalWorkspace 将 LocalSandbox 适配为 port.Workspace 接口。
type LocalWorkspace struct {
	sb *LocalSandbox
}

// NewLocalWorkspace 基于 LocalSandbox 创建 Workspace 适配器。
func NewLocalWorkspace(sb *LocalSandbox) *LocalWorkspace {
	return &LocalWorkspace{sb: sb}
}

func (w *LocalWorkspace) ReadFile(_ context.Context, path string) ([]byte, error) {
	return w.sb.ReadFile(path)
}

func (w *LocalWorkspace) WriteFile(_ context.Context, path string, content []byte) error {
	return w.sb.WriteFile(path, content)
}

func (w *LocalWorkspace) ListFiles(_ context.Context, pattern string) ([]string, error) {
	return w.sb.ListFiles(pattern)
}

func (w *LocalWorkspace) Stat(_ context.Context, path string) (port.FileInfo, error) {
	resolved, err := w.sb.ResolvePath(path)
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

func (w *LocalWorkspace) DeleteFile(_ context.Context, path string) error {
	resolved, err := w.sb.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

// LocalExecutor 将 LocalSandbox 适配为 port.Executor 接口。
type LocalExecutor struct {
	sb *LocalSandbox
}

// NewLocalExecutor 基于 LocalSandbox 创建 Executor 适配器。
func NewLocalExecutor(sb *LocalSandbox) *LocalExecutor {
	return &LocalExecutor{sb: sb}
}

func (e *LocalExecutor) Execute(ctx context.Context, cmd string, args []string) (port.ExecOutput, error) {
	out, err := e.sb.Execute(ctx, cmd, args)
	return port.ExecOutput{
		Stdout:   out.Stdout,
		Stderr:   out.Stderr,
		ExitCode: out.ExitCode,
	}, err
}
