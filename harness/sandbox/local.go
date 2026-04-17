package sandbox

import (
	"context"
	"fmt"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/workspace"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

	// First check: fast path before symlink resolution.
	if !s.isWithinAllowed(abs) {
		return "", fmt.Errorf("path %q escapes sandbox (root=%s)", path, s.root)
	}

	// Second check: resolve symlinks and verify again to prevent symlink-based escapes.
	resolved, err := resolveExistingPath(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks in %q: %w", path, err)
	}
	if !s.isWithinAllowed(resolved) {
		return "", fmt.Errorf("path %q (resolved: %s) escapes sandbox via symlink", path, resolved)
	}
	return resolved, nil
}

// isWithinAllowed reports whether p is within any of the sandbox's allowed paths.
func (s *LocalSandbox) isWithinAllowed(p string) bool {
	for _, allowed := range s.limits.AllowedPaths {
		if p == allowed || strings.HasPrefix(p, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *LocalSandbox) ListFiles(pattern string) ([]string, error) {
	if err := validateSandboxPattern(pattern); err != nil {
		return nil, err
	}
	// 支持 ** 递归匹配
	if strings.Contains(pattern, "**") {
		return s.listFilesRecursive(pattern)
	}
	fullPattern := filepath.Join(s.root, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, err
	}
	return s.filterListedPaths(matches)
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
	if _, err := s.ResolvePath(searchRoot); err != nil {
		return nil, err
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
	if err != nil {
		return nil, err
	}
	return s.filterListedPaths(matches)
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

func (s *LocalSandbox) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.limits.CommandTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := req.Command
	args := append([]string(nil), req.Args...)
	if strings.TrimSpace(cmd) == "" {
		return workspace.ExecOutput{}, fmt.Errorf("command is required")
	}
	shellWrapped := len(args) == 0 && needsShell(cmd)

	// 如果无参数且命令包含 shell 特殊字符，自动用 shell 包装。
	// 这样 LLM 可以直接发送 "ls -la" 或 "go build ./..." 等完整 shell 命令。
	if shellWrapped {
		if err := rejectShellChaining(cmd); err != nil {
			return workspace.ExecOutput{}, err
		}
		if runtime.GOOS == "windows" {
			args = []string{"/C", cmd}
			cmd = "cmd"
		} else {
			args = []string{"-c", cmd}
			cmd = "sh"
		}
	}

	// 防止通过显式传入 sh/bash 等 binary + -c 参数绕过 shell chaining 检查。
	if err := rejectShellBinaryArgs(cmd, args); err != nil {
		return workspace.ExecOutput{}, err
	}

	c := exec.CommandContext(ctx, cmd, args...)
	workDir := s.root
	if wd := strings.TrimSpace(req.WorkingDir); wd != "" {
		resolved, err := s.ResolvePath(wd)
		if err != nil {
			return workspace.ExecOutput{}, err
		}
		workDir = resolved
	}
	if len(req.AllowedPaths) > 0 {
		allowedRoots, err := s.resolveAllowedRoots(req.AllowedPaths)
		if err != nil {
			return workspace.ExecOutput{}, err
		}
		if !isWithinAllowedRoots(workDir, allowedRoots) {
			return workspace.ExecOutput{}, fmt.Errorf("working directory %q is outside allowed execution paths", workDir)
		}
		if shellWrapped {
			return workspace.ExecOutput{}, fmt.Errorf("shell-form commands are not allowed when execution paths are restricted; provide structured command and args")
		}
		if err := validateExecPathAccess(cmd, args, workDir, allowedRoots); err != nil {
			return workspace.ExecOutput{}, err
		}
	}
	c.Dir = workDir

	env, customized := buildCommandEnv(req)
	outputMeta := workspace.ExecOutput{}
	if len(req.Network.AllowHosts) > 0 {
		return workspace.ExecOutput{}, fmt.Errorf("network host allowlists are not supported by the local sandbox executor")
	}
	if req.Network.Mode == workspace.ExecNetworkDisabled {
		if req.Network.PreferHardBlock && !req.Network.AllowSoftLimit {
			return workspace.ExecOutput{}, fmt.Errorf("hard network isolation is unavailable in local sandbox")
		}
		if req.Network.PreferHardBlock {
			slog.Warn("network isolation degraded to soft limit",
				"reason", "hard isolation unavailable in local sandbox",
				"command", req.Command)
		}
		if !customized {
			env = envMapToSlice(SafeInheritedEnvironment())
			customized = true
		}
		env = applySoftNetworkLimit(env)
		outputMeta.Enforcement = io.EnforcementSoftLimit
		outputMeta.Degraded = true
		outputMeta.Details = "hard network isolation unavailable in local sandbox; applied soft network limit via environment"
	}
	if customized {
		c.Env = env
	}

	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	out := Output{
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		Enforcement: outputMeta.Enforcement,
		Degraded:    outputMeta.Degraded,
		Details:     outputMeta.Details,
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

func buildCommandEnv(req workspace.ExecRequest) ([]string, bool) {
	customized := req.ClearEnv || len(req.Env) > 0
	if !customized {
		return nil, false
	}
	env := []string{}
	if !req.ClearEnv {
		env = os.Environ()
	}
	for key, value := range req.Env {
		env = upsertEnv(env, key, value)
	}
	return env, true
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func applySoftNetworkLimit(env []string) []string {
	limited := append([]string(nil), env...)
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
	} {
		value := ""
		if strings.EqualFold(key, "NO_PROXY") {
			value = "*"
		}
		limited = upsertEnv(limited, key, value)
	}
	return limited
}

func (s *LocalSandbox) resolveAllowedRoots(paths []string) ([]string, error) {
	roots := make([]string, 0, len(paths))
	for _, candidate := range paths {
		resolved, err := s.ResolvePath(candidate)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	return roots, nil
}

func (s *LocalSandbox) filterListedPaths(paths []string) ([]string, error) {
	filtered := make([]string, 0, len(paths))
	for _, match := range paths {
		rel, err := filepath.Rel(s.root, match)
		if err != nil {
			return nil, err
		}
		if _, err := s.ResolvePath(rel); err != nil {
			continue
		}
		filtered = append(filtered, match)
	}
	slices.Sort(filtered)
	return filtered, nil
}

func isWithinAllowedRoots(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func resolveExistingPath(path string) (string, error) {
	path = filepath.Clean(path)
	missing := make([]string, 0, 4)
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for _, part := range missing {
				resolved = filepath.Join(resolved, part)
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append([]string{filepath.Base(current)}, missing...)
		current = parent
	}
}

func validateSandboxPattern(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	if filepath.IsAbs(pattern) {
		return fmt.Errorf("absolute patterns are not allowed: %q", pattern)
	}
	for _, part := range strings.FieldsFunc(filepath.Clean(pattern), func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return fmt.Errorf("pattern %q escapes sandbox", pattern)
		}
	}
	return nil
}

func validateExecPathAccess(cmd string, args []string, workDir string, allowedRoots []string) error {
	if looksLikePathToken(cmd) {
		resolved, err := resolveExecPathCandidate(cmd, workDir)
		if err != nil {
			return err
		}
		if !isWithinAllowedRoots(resolved, allowedRoots) {
			return fmt.Errorf("command path %q is outside allowed execution paths", cmd)
		}
	}
	for _, arg := range args {
		candidate := pathCandidateFromArg(arg)
		if candidate == "" {
			continue
		}
		resolved, err := resolveExecPathCandidate(candidate, workDir)
		if err != nil {
			return err
		}
		if !isWithinAllowedRoots(resolved, allowedRoots) {
			return fmt.Errorf("command argument path %q is outside allowed execution paths", arg)
		}
	}
	return nil
}

func pathCandidateFromArg(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ""
	}
	if idx := strings.Index(arg, "="); idx > 0 && strings.HasPrefix(arg, "-") {
		if candidate := strings.TrimSpace(arg[idx+1:]); looksLikePathToken(candidate) {
			return candidate
		}
		return ""
	}
	if looksLikePathToken(arg) {
		return arg
	}
	return ""
}

func looksLikePathToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	if strings.Contains(value, "://") {
		return false
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return true
	}
	if value == "." || value == ".." {
		return true
	}
	if strings.HasPrefix(value, "."+string(filepath.Separator)) || strings.HasPrefix(value, ".."+string(filepath.Separator)) {
		return true
	}
	return strings.ContainsAny(value, `/\`)
}

func resolveExecPathCandidate(candidate, workDir string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", fmt.Errorf("empty path candidate")
	}
	if filepath.IsAbs(candidate) || filepath.VolumeName(candidate) != "" {
		return filepath.Clean(candidate), nil
	}
	return filepath.Clean(filepath.Join(workDir, candidate)), nil
}

func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

// needsShell 检查命令是否需要 shell 包装（包含空格、管道、重定向等）。
func needsShell(cmd string) bool {
	return strings.ContainsAny(cmd, " \t|><&;$`")
}

// rejectShellChaining rejects commands that contain shell chaining or injection
// operators (;, &&, ||, |, backtick, $(...), ${...}, >, <). These operators allow
// composing multiple commands and must not be auto-wrapped via sh -c.
// Simple word-splitting (spaces/tabs) is safe and is handled by the caller.
func rejectShellChaining(cmd string) error {
	dangerous := []struct {
		seq  string
		name string
	}{
		{";", "command separator ';'"},
		{"&&", "operator '&&'"},
		{"||", "operator '||'"},
		{"|", "pipe '|'"},
		{"`", "backtick command substitution"},
		{"$(", "subshell '$()'"},
		{"${", "variable substitution '${}'"},
		{">", "output redirection '>'"},
		{"<", "input redirection '<'"},
	}
	for _, d := range dangerous {
		if strings.Contains(cmd, d.seq) {
			return fmt.Errorf(
				"command contains %s; use the 'args' field to pass arguments separately",
				d.name,
			)
		}
	}
	return nil
}

// shellBinaries is the set of binary names that spawn an interactive shell.
// Commands with these names must have their arguments validated for chaining
// operators even when the shell-wrap path is not taken.
var shellBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "ksh": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true, "pwsh": true, "pwsh.exe": true,
}

// rejectShellBinaryArgs guards against bypassing rejectShellChaining by
// explicitly passing a shell binary (e.g. cmd="sh", args=["-c", "ls; rm -rf /"]).
// For each argument passed to a known shell binary it checks for chaining operators.
func rejectShellBinaryArgs(cmd string, args []string) error {
	base := strings.ToLower(filepath.Base(cmd))
	if !shellBinaries[base] {
		return nil
	}
	for _, arg := range args {
		if err := rejectShellChaining(arg); err != nil {
			return fmt.Errorf("shell binary %q arg %q: %w", filepath.Base(cmd), arg, err)
		}
	}
	return nil
}

func (s *LocalSandbox) Limits() ResourceLimits {
	return s.limits
}

// LocalWorkspace 将 LocalSandbox 适配为 workspace.Workspace 接口。
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

func (w *LocalWorkspace) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, err := w.sb.ResolvePath(path)
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

func (w *LocalWorkspace) DeleteFile(_ context.Context, path string) error {
	resolved, err := w.sb.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

// LocalExecutor 将 LocalSandbox 适配为 workspace.Executor 接口。
type LocalExecutor struct {
	sb *LocalSandbox
}

// NewLocalExecutor 基于 LocalSandbox 创建 Executor 适配器。
func NewLocalExecutor(sb *LocalSandbox) *LocalExecutor {
	return &LocalExecutor{sb: sb}
}

func (e *LocalExecutor) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	return e.sb.Execute(ctx, req)
}
