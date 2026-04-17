// Package docker 提供基于 Docker 容器的 Workspace 实现。
// 所有命令通过 `docker run --rm` 执行，文件操作代理到宿主机工作目录。
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/workspace"
)

// DockerConfig 配置 Docker Workspace。
type DockerConfig struct {
	// Image 是容器镜像，例如 "ubuntu:22.04"。
	Image string
	// WorkDir 是宿主机上的工作目录，将挂载为容器内 /workspace。
	WorkDir string
	// Memory 是容器内存限制，例如 "512m"；空字符串不限制。
	Memory string
	// CPUs 是容器 CPU 配额，例如 "1.0"；空字符串不限制。
	CPUs string
	// Network 控制容器网络访问，"none" 禁用网络。
	Network string
	// Timeout 是每次命令执行的超时，默认 30s。
	Timeout time.Duration
	// MaxFileSize 是文件大小限制（字节）。
	MaxFileSize int64
}

// execFunc 是可替换的命令执行函数，方便测试。
type execFunc func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error)

// DockerWorkspace 通过 `docker run --rm` 执行命令，文件操作代理到宿主机目录。
type DockerWorkspace struct {
	cfg     DockerConfig
	workDir string // 已解析的绝对工作目录
	exec    execFunc
}

var _ workspace.Workspace = (*DockerWorkspace)(nil)

// New 创建 DockerWorkspace。
func New(cfg DockerConfig) (*DockerWorkspace, error) {
	if cfg.Image == "" {
		return nil, fmt.Errorf("docker workspace: Image is required")
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("docker workspace: resolve work dir: %w", err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = 10 * 1024 * 1024 // 10 MB
	}
	return &DockerWorkspace{
		cfg:     cfg,
		workDir: abs,
		exec:    defaultExecFunc,
	}, nil
}

// ── 安全边界 ──

// ResolvePath 解析路径并确保不逃逸工作目录。
func (d *DockerWorkspace) ResolvePath(path string) (string, error) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(d.workDir, path))
	}
	if abs != d.workDir && !strings.HasPrefix(abs, d.workDir+string(filepath.Separator)) {
		return "", fmt.Errorf("docker workspace: path %q escapes work dir %s", path, d.workDir)
	}
	return abs, nil
}

func (d *DockerWorkspace) Capabilities() workspace.Capabilities {
	return workspace.Capabilities{
		FileSystemIsolation: workspace.IsolationMethodContainer,
		NetworkIsolation:    workspace.IsolationMethodContainer,
		ProcessIsolation:    workspace.IsolationMethodContainer,
		ResourceEnforcement: true,
	}
}

func (d *DockerWorkspace) Policy() workspace.SecurityPolicy {
	return workspace.SecurityPolicy{}
}

func (d *DockerWorkspace) Limits() workspace.ResourceLimits {
	return workspace.ResourceLimits{
		MaxFileSize:    d.cfg.MaxFileSize,
		CommandTimeout: d.cfg.Timeout,
		AllowedPaths:   []string{d.workDir},
	}
}

// ── 文件操作 ──

// ListFiles 按 glob pattern 列出工作目录下的文件。
func (d *DockerWorkspace) ListFiles(_ context.Context, pattern string) ([]string, error) {
	fullPattern := filepath.Join(d.workDir, pattern)
	if strings.Contains(pattern, "**") {
		return d.listFilesRecursive(pattern)
	}
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(d.workDir, m)
		if err == nil {
			result = append(result, rel)
		}
	}
	return result, nil
}

func (d *DockerWorkspace) listFilesRecursive(pattern string) ([]string, error) {
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/\\")
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimLeft(parts[1], "/\\")
	}
	searchRoot := d.workDir
	if prefix != "" {
		searchRoot = filepath.Join(d.workDir, prefix)
	}
	var result []string
	err := filepath.WalkDir(searchRoot, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		if suffix == "" {
			rel, _ := filepath.Rel(d.workDir, path)
			result = append(result, rel)
			return nil
		}
		if ok, _ := filepath.Match(suffix, e.Name()); ok {
			rel, _ := filepath.Rel(d.workDir, path)
			result = append(result, rel)
		}
		return nil
	})
	return result, err
}

// ReadFile 从工作目录读取文件。
func (d *DockerWorkspace) ReadFile(_ context.Context, path string) ([]byte, error) {
	resolved, err := d.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

// WriteFile 写入文件到工作目录。
func (d *DockerWorkspace) WriteFile(_ context.Context, path string, content []byte) error {
	resolved, err := d.ResolvePath(path)
	if err != nil {
		return err
	}
	if d.cfg.MaxFileSize > 0 && int64(len(content)) > d.cfg.MaxFileSize {
		return fmt.Errorf("docker workspace: file size %d exceeds limit %d", len(content), d.cfg.MaxFileSize)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return fmt.Errorf("docker workspace: create parent dirs: %w", err)
	}
	return os.WriteFile(resolved, content, 0644)
}

func (d *DockerWorkspace) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, err := d.ResolvePath(path)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	return workspace.FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

func (d *DockerWorkspace) DeleteFile(_ context.Context, path string) error {
	resolved, err := d.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

// ── 命令执行 ──

// Execute 通过 `docker run --rm` 在容器内执行命令。
func (d *DockerWorkspace) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = d.cfg.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if strings.TrimSpace(req.Command) == "" {
		return workspace.ExecOutput{}, fmt.Errorf("docker workspace: command is required")
	}

	dockerArgs := d.buildDockerArgs(req)

	stdout, stderr, exitCode, err := d.exec(ctx, "docker", dockerArgs...)
	out := workspace.ExecOutput{
		Stdout:   string(stdout),
		Stderr:   string(stderr),
		ExitCode: exitCode,
	}
	if err != nil && exitCode == 0 {
		return out, fmt.Errorf("docker workspace: exec: %w", err)
	}
	return out, nil
}

// buildDockerArgs 构建 docker run 命令参数。
func (d *DockerWorkspace) buildDockerArgs(req workspace.ExecRequest) []string {
	args := []string{"run", "--rm"}

	// Security hardening
	args = append(args, "--security-opt", "no-new-privileges")
	args = append(args, "--cap-drop", "ALL")
	args = append(args, "--pids-limit", "256")
	args = append(args, "--read-only")
	args = append(args, "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m")

	// 挂载工作目录
	args = append(args, "-v", d.workDir+":/workspace")
	workDir := "/workspace"
	if req.WorkingDir != "" {
		workDir = "/workspace/" + strings.TrimPrefix(filepath.ToSlash(req.WorkingDir), "/")
	}
	args = append(args, "-w", workDir)

	// 资源限制
	if d.cfg.Memory != "" {
		args = append(args, "--memory", d.cfg.Memory)
	}
	if d.cfg.CPUs != "" {
		args = append(args, "--cpus", d.cfg.CPUs)
	}

	// 网络
	network := d.cfg.Network
	if req.Network.Mode == workspace.ExecNetworkDisabled {
		network = "none"
	}
	if network != "" {
		args = append(args, "--network", network)
	}

	// 环境变量
	for k, v := range req.Env {
		args = append(args, "-e", k+"="+v)
	}

	// 镜像
	args = append(args, d.cfg.Image)

	// 命令
	args = append(args, req.Command)
	args = append(args, req.Args...)

	return args
}

// SetExecFunc 替换底层命令执行函数，仅用于测试。
func (d *DockerWorkspace) SetExecFunc(fn func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error)) {
	d.exec = fn
}

// defaultExecFunc 使用 os/exec 执行命令。
func defaultExecFunc(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	c := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	exitCode := 0
	if c.ProcessState != nil {
		exitCode = c.ProcessState.ExitCode()
	}
	return stdout.Bytes(), stderr.Bytes(), exitCode, err
}
