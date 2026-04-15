package sandbox

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSandboxResolvePath(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	// 正常相对路径
	resolved, err := s.ResolvePath("foo/bar.txt")
	if err != nil {
		t.Fatalf("ResolvePath relative: %v", err)
	}
	expected := filepath.Join(dir, "foo", "bar.txt")
	if resolved != expected {
		t.Fatalf("resolved = %q, want %q", resolved, expected)
	}

	// 路径逃逸
	_, err = s.ResolvePath("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestLocalSandboxReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	content := []byte("hello world")
	if err := s.WriteFile("test.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := s.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestLocalSandboxWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	if err := s.WriteFile("a/b/c.txt", []byte("nested")); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c.txt")); err != nil {
		t.Fatalf("nested file should exist: %v", err)
	}
}

func TestLocalSandboxFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir, WithMaxFileSize(10))

	if err := s.WriteFile("big.txt", make([]byte, 11)); err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestLocalSandboxExecute(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	// 使用跨平台兼容的命令
	var out Output
	var err error
	if isWindows() {
		out, err = s.Execute(context.Background(), workspace.ExecRequest{Command: "cmd", Args: []string{"/C", "echo", "hello"}})
	} else {
		out, err = s.Execute(context.Background(), workspace.ExecRequest{Command: "echo", Args: []string{"hello"}})
	}
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", out.ExitCode)
	}
}

func isWindows() bool {
	return filepath.Separator == '\\'
}

func TestLocalSandboxListFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := s.ListFiles("*.txt")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListFiles len = %d, want 2", len(files))
	}
}

func TestLocalSandboxListFilesRecursive(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	// 创建嵌套目录结构
	if err := os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("package src"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "pkg", "util.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	// **/*.go 应匹配所有 .go 文件
	files, err := s.ListFiles("**/*.go")
	if err != nil {
		t.Fatalf("ListFiles recursive: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 .go files, got %d: %v", len(files), files)
	}

	// **/* 应匹配所有文件
	all, err := s.ListFiles("**/*")
	if err != nil {
		t.Fatalf("ListFiles all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 files, got %d: %v", len(all), all)
	}
}

func TestLocalSandboxListFilesRejectsTraversalPattern(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)
	if _, err := s.ListFiles("..\\*.txt"); err == nil {
		t.Fatal("expected traversal pattern rejection")
	}
}

func TestLocalSandboxExecuteShellCommand(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	// 测试 shell 命令自动包装（命令包含空格）
	var out Output
	var err error
	if isWindows() {
		out, err = s.Execute(context.Background(), workspace.ExecRequest{Command: "echo hello world"})
	} else {
		out, err = s.Execute(context.Background(), workspace.ExecRequest{Command: "echo hello world"})
	}
	if err != nil {
		t.Fatalf("Execute shell: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0, stderr: %s", out.ExitCode, out.Stderr)
	}
	if !contains(out.Stdout, "hello world") {
		t.Fatalf("stdout = %q, expected to contain 'hello world'", out.Stdout)
	}
}

func TestLocalSandboxExecuteWorkingDirMustBeAllowed(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)
	if err := os.MkdirAll(filepath.Join(dir, "allowed"), 0755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "blocked"), 0755); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}

	_, err := s.Execute(context.Background(), workspace.ExecRequest{
		Command:      commandForNoop(),
		Args:         argsForNoop(),
		WorkingDir:   "blocked",
		AllowedPaths: []string{"allowed"},
	})
	if err == nil {
		t.Fatal("expected allowed path enforcement error")
	}
}

func TestLocalSandboxWriteFileRejectsSymlinkParentEscape(t *testing.T) {
	if isWindows() {
		t.Skip("symlink creation requires elevated privileges on some Windows setups")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	s, _ := NewLocal(dir)
	linkPath := filepath.Join(dir, "linked")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := s.WriteFile(filepath.Join("linked", "escape.txt"), []byte("nope")); err == nil {
		t.Fatal("expected symlink parent escape rejection")
	}
}

func TestLocalSandboxExecuteClearEnvAndInjectEnv(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	var req workspace.ExecRequest
	if isWindows() {
		req = workspace.ExecRequest{
			Command:  "cmd",
			Args:     []string{"/C", "echo", "%MOSS_SANDBOX_TEST%"},
			ClearEnv: true,
			Env:      map[string]string{"MOSS_SANDBOX_TEST": "sandboxed"},
		}
	} else {
		req = workspace.ExecRequest{
			Command:  "sh",
			Args:     []string{"-c", "printf %s \"$MOSS_SANDBOX_TEST\""},
			ClearEnv: true,
			Env:      map[string]string{"MOSS_SANDBOX_TEST": "sandboxed"},
		}
	}
	out, err := s.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute env: %v", err)
	}
	if !contains(out.Stdout, "sandboxed") {
		t.Fatalf("stdout = %q, want injected env value", out.Stdout)
	}
}

func TestLocalSandboxExecuteDisabledNetworkFallsBackToSoftLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	out, err := s.Execute(context.Background(), workspace.ExecRequest{
		Command: commandForNoop(),
		Args:    argsForNoop(),
		Network: workspace.ExecNetworkPolicy{
			Mode:            workspace.ExecNetworkDisabled,
			PreferHardBlock: true,
			AllowSoftLimit:  true,
		},
	})
	if err != nil {
		t.Fatalf("Execute disabled network: %v", err)
	}
	if out.Enforcement != io.EnforcementSoftLimit {
		t.Fatalf("enforcement = %q, want %q", out.Enforcement, io.EnforcementSoftLimit)
	}
	if !out.Degraded {
		t.Fatal("expected degraded soft-limit marker")
	}
	if out.Details == "" {
		t.Fatal("expected degradation details")
	}
}

func TestLocalSandboxExecuteDisabledNetworkHardBlockOnlyFails(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)

	_, err := s.Execute(context.Background(), workspace.ExecRequest{
		Command: commandForNoop(),
		Args:    argsForNoop(),
		Network: workspace.ExecNetworkPolicy{
			Mode:            workspace.ExecNetworkDisabled,
			PreferHardBlock: true,
			AllowSoftLimit:  false,
		},
	})
	if err == nil {
		t.Fatal("expected hard-block unavailable error")
	}
}

func TestLocalSandboxExecuteRejectsPathArgsOutsideAllowedRoots(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)
	if err := os.MkdirAll(filepath.Join(dir, "allowed"), 0o755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	_, err := s.Execute(context.Background(), workspace.ExecRequest{
		Command:      commandForNoop(),
		Args:         append(argsForNoop(), filepath.Join("..", "blocked.txt")),
		WorkingDir:   "allowed",
		AllowedPaths: []string{"allowed"},
	})
	if err == nil {
		t.Fatal("expected outside-allowed-path argument rejection")
	}
}

func TestLocalSandboxExecuteRejectsAllowHosts(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocal(dir)
	_, err := s.Execute(context.Background(), workspace.ExecRequest{
		Command: commandForNoop(),
		Args:    argsForNoop(),
		Network: workspace.ExecNetworkPolicy{
			Mode:       workspace.ExecNetworkEnabled,
			AllowHosts: []string{"example.com"},
		},
	})
	if err == nil {
		t.Fatal("expected allow-hosts unsupported error")
	}
}

func commandForNoop() string {
	if isWindows() {
		return "cmd"
	}
	return "echo"
}

func argsForNoop() []string {
	if isWindows() {
		return []string{"/C", "echo", "ok"}
	}
	return []string{"ok"}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
