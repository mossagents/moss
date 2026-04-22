package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/workspace"
	"log/slog"
)

// LocalWorkspace 是基于本地文件系统的 Workspace 实现。
// 提供路径验证、文件 I/O、命令执行和资源限制。
type LocalWorkspace struct {
	root   string
	limits workspace.ResourceLimits
	policy workspace.SecurityPolicy
}

var _ workspace.Workspace = (*LocalWorkspace)(nil)

// NewLocalWorkspace 在指定根目录创建 LocalWorkspace。
func NewLocalWorkspace(root string, opts ...Option) (*LocalWorkspace, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	w := &LocalWorkspace{
		root: absRoot,
		limits: workspace.ResourceLimits{
			MaxFileSize:    10 * 1024 * 1024, // 10 MB
			CommandTimeout: 30 * time.Second,
			AllowedPaths:   []string{absRoot},
		},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w, nil
}

// Option 配置 LocalWorkspace。
type Option func(*LocalWorkspace)

// WithMaxFileSize 设置文件大小限制。
func WithMaxFileSize(n int64) Option {
	return func(w *LocalWorkspace) { w.limits.MaxFileSize = n }
}

// WithCommandTimeout 设置命令执行超时。
func WithCommandTimeout(d time.Duration) Option {
	return func(w *LocalWorkspace) { w.limits.CommandTimeout = d }
}

// WithAllowedPaths 设置额外的路径白名单。
func WithAllowedPaths(paths ...string) Option {
	return func(w *LocalWorkspace) {
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err == nil {
				w.limits.AllowedPaths = append(w.limits.AllowedPaths, abs)
			}
		}
	}
}

// WithSecurityPolicy 设置安全策略。
func WithSecurityPolicy(p workspace.SecurityPolicy) Option {
	return func(w *LocalWorkspace) { w.policy = p }
}

// WithResourceLimits 覆盖资源限制。
func WithResourceLimits(l workspace.ResourceLimits) Option {
	return func(w *LocalWorkspace) { w.limits = l }
}

// Root 返回 workspace 根目录的绝对路径。
func (w *LocalWorkspace) Root() string {
	return w.root
}

// ── 安全边界 ──

func (w *LocalWorkspace) ResolvePath(path string) (string, error) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(w.root, path))
	}

	// First check: fast path before symlink resolution.
	if !w.isWithinAllowed(abs) {
		return "", fmt.Errorf("path %q escapes workspace (root=%s)", path, w.root)
	}

	// Second check: resolve symlinks and verify again to prevent symlink-based escapes.
	resolved, err := resolveExistingPath(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks in %q: %w", path, err)
	}
	if !w.isWithinAllowed(resolved) {
		return "", fmt.Errorf("path %q (resolved: %s) escapes workspace via symlink", path, resolved)
	}
	return resolved, nil
}

// isWithinAllowed reports whether p is within any of the workspace's allowed paths.
func (w *LocalWorkspace) isWithinAllowed(p string) bool {
	for _, allowed := range w.limits.AllowedPaths {
		if p == allowed || strings.HasPrefix(p, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (w *LocalWorkspace) Capabilities() workspace.Capabilities {
	return workspace.Capabilities{
		FileSystemIsolation: workspace.IsolationMethodPathCheck,
		NetworkIsolation:    workspace.IsolationMethodProxy,
		ProcessIsolation:    workspace.IsolationMethodNone,
		ResourceEnforcement: false,
		GovernanceOnly:      true,
		HardSandbox:         false,
	}
}

func (w *LocalWorkspace) Policy() workspace.SecurityPolicy {
	return w.policy
}

func (w *LocalWorkspace) Limits() workspace.ResourceLimits {
	return w.limits
}

// ── 文件操作 ──

// isProtected reports whether absPath falls under any ProtectedPaths prefix.
func (w *LocalWorkspace) isProtected(absPath string) bool {
	for _, pp := range w.policy.ProtectedPaths {
		var protectedAbs string
		if filepath.IsAbs(pp) {
			protectedAbs = filepath.Clean(pp)
		} else {
			protectedAbs = filepath.Clean(filepath.Join(w.root, pp))
		}
		if absPath == protectedAbs || strings.HasPrefix(absPath, protectedAbs+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isDenyRead reports whether absPath matches any DenyReadPatterns glob.
func (w *LocalWorkspace) isDenyRead(absPath string) bool {
	rel, err := filepath.Rel(w.root, absPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, pattern := range w.policy.DenyReadPatterns {
		pattern = filepath.ToSlash(pattern)
		if matched, _ := filepath.Match(pattern, rel); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(rel)); matched {
			return true
		}
	}
	return false
}

// resolveFileAccess returns the effective access mode for absPath based on FileRules.
// Returns FileAccessWrite (default full access) if no rule matches.
func (w *LocalWorkspace) resolveFileAccess(absPath string) workspace.FileAccessMode {
	rel, err := filepath.Rel(w.root, absPath)
	if err != nil {
		return workspace.FileAccessWrite
	}
	rel = filepath.ToSlash(rel)
	for _, rule := range w.policy.FileRules {
		rulePattern := filepath.ToSlash(rule.Path)
		if matched, _ := filepath.Match(rulePattern, rel); matched {
			return rule.Access
		}
		if matched, _ := filepath.Match(rulePattern, filepath.Base(rel)); matched {
			return rule.Access
		}
	}
	return workspace.FileAccessWrite
}

func (w *LocalWorkspace) ReadFile(_ context.Context, path string) ([]byte, error) {
	resolved, err := w.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	if w.isDenyRead(resolved) {
		return nil, fmt.Errorf("read access denied for %q", path)
	}
	return os.ReadFile(resolved)
}

func (w *LocalWorkspace) WriteFile(_ context.Context, path string, content []byte) error {
	resolved, err := w.ResolvePath(path)
	if err != nil {
		return err
	}
	if w.policy.ReadOnly {
		return fmt.Errorf("workspace is read-only")
	}
	if w.isProtected(resolved) {
		return fmt.Errorf("path %q is protected (read-only)", path)
	}
	if access := w.resolveFileAccess(resolved); access == workspace.FileAccessNone {
		return fmt.Errorf("write access denied for %q", path)
	} else if access == workspace.FileAccessRead {
		return fmt.Errorf("path %q is read-only per file access rule", path)
	}
	if w.limits.MaxFileSize > 0 && int64(len(content)) > w.limits.MaxFileSize {
		return fmt.Errorf("file size %d exceeds limit %d", len(content), w.limits.MaxFileSize)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}
	return os.WriteFile(resolved, content, 0644)
}

func (w *LocalWorkspace) ListFiles(_ context.Context, pattern string) ([]string, error) {
	if err := validateSandboxPattern(pattern); err != nil {
		return nil, err
	}
	// 支持 ** 递归匹配
	if strings.Contains(pattern, "**") {
		return w.listFilesRecursive(pattern)
	}
	fullPattern := filepath.Join(w.root, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, err
	}
	return w.filterListedPaths(matches)
}

func (w *LocalWorkspace) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, err := w.ResolvePath(path)
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
	resolved, err := w.ResolvePath(path)
	if err != nil {
		return err
	}
	if w.policy.ReadOnly {
		return fmt.Errorf("workspace is read-only")
	}
	if w.isProtected(resolved) {
		return fmt.Errorf("path %q is protected (read-only)", path)
	}
	return os.Remove(resolved)
}

// listFilesRecursive 用 WalkDir 实现 ** 递归 glob 匹配。
func (w *LocalWorkspace) listFilesRecursive(pattern string) ([]string, error) {
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/\\")
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimLeft(parts[1], "/\\")
	}

	searchRoot := w.root
	if prefix != "" {
		searchRoot = filepath.Join(w.root, prefix)
	}
	if _, err := w.ResolvePath(searchRoot); err != nil {
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
	return w.filterListedPaths(matches)
}

func (w *LocalWorkspace) filterListedPaths(paths []string) ([]string, error) {
	filtered := make([]string, 0, len(paths))
	for _, match := range paths {
		rel, err := filepath.Rel(w.root, match)
		if err != nil {
			return nil, err
		}
		if _, err := w.ResolvePath(rel); err != nil {
			continue
		}
		filtered = append(filtered, match)
	}
	slices.Sort(filtered)
	return filtered, nil
}

// ── 命令执行 ──

func (w *LocalWorkspace) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	if req.IsolationLevel == workspace.IsolationSandbox && !w.Capabilities().SupportsHardSandbox() {
		out := workspace.ExecOutput{
			Enforcement: io.EnforcementHardBlock,
			Details:     "IsolationSandbox requires a hard-sandbox backend; local workspace only provides governance-only host execution",
		}
		return out, fmt.Errorf("hard sandbox isolation is unavailable in local workspace")
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = w.limits.CommandTimeout
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
	c.SysProcAttr = newProcessGroupAttr()
	workDir := w.root
	if wd := strings.TrimSpace(req.WorkingDir); wd != "" {
		resolved, err := w.ResolvePath(wd)
		if err != nil {
			return workspace.ExecOutput{}, err
		}
		workDir = resolved
	}
	if len(req.AllowedPaths) > 0 {
		allowedRoots, err := w.resolveAllowedRoots(req.AllowedPaths)
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
		return workspace.ExecOutput{}, fmt.Errorf("network host allowlists are not supported by the local workspace executor")
	}
	if req.Network.Mode == workspace.ExecNetworkDisabled {
		if req.Network.PreferHardBlock && !req.Network.AllowSoftLimit {
			return workspace.ExecOutput{}, fmt.Errorf("hard network isolation is unavailable in local workspace; restricted mode on this backend is governance-only")
		}
		if req.Network.PreferHardBlock {
			slog.Warn("network isolation degraded to soft limit",
				"reason", "local workspace is governance-only",
				"command", req.Command)
		}
		if !customized {
			env = envMapToSlice(SafeInheritedEnvironment())
			customized = true
		}
		env = applySoftNetworkLimit(env)
		outputMeta.Enforcement = io.EnforcementSoftLimit
		outputMeta.Degraded = true
		outputMeta.Details = "local workspace is governance-only; applied soft network limit via environment because hard network isolation is unavailable"
	}
	if customized {
		c.Env = env
	}

	var stdout, stderr strings.Builder
	maxOutput := w.limits.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 10 * 1024 * 1024 // 10 MB default
	}
	stdoutLimited := newLimitedWriter(&stdout, maxOutput)
	stderrLimited := newLimitedWriter(&stderr, maxOutput)
	c.Stdout = stdoutLimited
	c.Stderr = stderrLimited

	err := c.Run()

	// On context cancellation (timeout), kill the entire process group
	// to avoid orphaned child processes.
	if ctx.Err() != nil && c.Process != nil {
		killProcessGroup(c.Process)
	}

	out := workspace.ExecOutput{
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		Enforcement: outputMeta.Enforcement,
		Degraded:    outputMeta.Degraded,
		Details:     outputMeta.Details,
	}
	if stdoutLimited.Truncated() || stderrLimited.Truncated() {
		out.Stderr += "\n[output truncated: exceeded limit]"
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

func (w *LocalWorkspace) resolveAllowedRoots(paths []string) ([]string, error) {
	roots := make([]string, 0, len(paths))
	for _, candidate := range paths {
		resolved, err := w.ResolvePath(candidate)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	return roots, nil
}

// ── 共享辅助函数 ──

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
			return fmt.Errorf("pattern %q escapes workspace", pattern)
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
var shellBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "ksh": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true, "pwsh": true, "pwsh.exe": true,
}

// rejectShellBinaryArgs guards against bypassing rejectShellChaining by
// explicitly passing a shell binary (e.g. cmd="sh", args=["-c", "ls; rm -rf /"]).
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
