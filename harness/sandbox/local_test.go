package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/workspace"
)

func TestLocalWorkspaceResolvePath(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocalWorkspace(dir)
	if err != nil {
		t.Fatalf("NewLocalWorkspace: %v", err)
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

func TestLocalWorkspaceReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()

	content := []byte("hello world")
	if err := s.WriteFile(ctx, "test.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := s.ReadFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestLocalWorkspaceWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()

	if err := s.WriteFile(ctx, "a/b/c.txt", []byte("nested")); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c.txt")); err != nil {
		t.Fatalf("nested file should exist: %v", err)
	}
}

func TestLocalWorkspaceFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithMaxFileSize(10))
	ctx := context.Background()

	if err := s.WriteFile(ctx, "big.txt", make([]byte, 11)); err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestLocalWorkspaceExecute(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)

	// 使用跨平台兼容的命令
	var out workspace.ExecOutput
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

func TestLocalWorkspaceListFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := s.ListFiles(ctx, "*.txt")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListFiles len = %d, want 2", len(files))
	}
}

func TestLocalWorkspaceListFilesRecursive(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()

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
	files, err := s.ListFiles(ctx, "**/*.go")
	if err != nil {
		t.Fatalf("ListFiles recursive: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 .go files, got %d: %v", len(files), files)
	}

	// **/* 应匹配所有文件
	all, err := s.ListFiles(ctx, "**/*")
	if err != nil {
		t.Fatalf("ListFiles all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 files, got %d: %v", len(all), all)
	}
}

func TestLocalWorkspaceListFilesRejectsTraversalPattern(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()
	if _, err := s.ListFiles(ctx, "..\\*.txt"); err == nil {
		t.Fatal("expected traversal pattern rejection")
	}
}

func TestLocalWorkspaceExecuteShellCommand(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)

	// 测试 shell 命令自动包装（命令包含空格）
	var out workspace.ExecOutput
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

func TestLocalWorkspaceExecuteWorkingDirMustBeAllowed(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
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

func TestLocalWorkspaceWriteFileRejectsSymlinkParentEscape(t *testing.T) {
	if isWindows() {
		t.Skip("symlink creation requires elevated privileges on some Windows setups")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
	ctx := context.Background()
	linkPath := filepath.Join(dir, "linked")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := s.WriteFile(ctx, filepath.Join("linked", "escape.txt"), []byte("nope")); err == nil {
		t.Fatal("expected symlink parent escape rejection")
	}
}

func TestLocalWorkspaceExecuteClearEnvAndInjectEnv(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)

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

func TestLocalWorkspaceExecuteDisabledNetworkFallsBackToSoftLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)

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

func TestLocalWorkspaceExecuteDisabledNetworkHardBlockOnlyFails(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)

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

func TestLocalWorkspaceExecuteRejectsPathArgsOutsideAllowedRoots(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
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

func TestLocalWorkspaceExecuteRejectsAllowHosts(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir)
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

// ── SecurityPolicy enforcement tests ──

func TestLocalWorkspaceReadOnlyRejectsWrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{ReadOnly: true}))
	ctx := context.Background()
	if err := s.WriteFile(ctx, "test.txt", []byte("x")); err == nil {
		t.Fatal("expected read-only rejection")
	}
}

func TestLocalWorkspaceReadOnlyRejectsDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{ReadOnly: true}))
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("x"), 0644)
	if err := s.DeleteFile(ctx, "test.txt"); err == nil {
		t.Fatal("expected read-only rejection for delete")
	}
}

func TestLocalWorkspaceProtectedPathRejectsWrite(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0755)
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		ProtectedPaths: []string{".git"},
	}))
	ctx := context.Background()
	if err := s.WriteFile(ctx, ".git/hooks/pre-commit", []byte("#!/bin/sh")); err == nil {
		t.Fatal("expected protected path rejection")
	}
}

func TestLocalWorkspaceProtectedPathRejectsDelete(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0644)
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		ProtectedPaths: []string{".git"},
	}))
	ctx := context.Background()
	if err := s.DeleteFile(ctx, ".git/config"); err == nil {
		t.Fatal("expected protected path rejection for delete")
	}
}

func TestLocalWorkspaceDenyReadPatterns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0644)
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		DenyReadPatterns: []string{".env"},
	}))
	ctx := context.Background()
	if _, err := s.ReadFile(ctx, ".env"); err == nil {
		t.Fatal("expected deny-read rejection for .env")
	}
}

func TestLocalWorkspaceFileAccessRuleReadOnly(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		FileRules: []workspace.FileAccessRule{
			{Path: "*.lock", Access: workspace.FileAccessRead},
		},
	}))
	ctx := context.Background()
	if err := s.WriteFile(ctx, "go.lock", []byte("x")); err == nil {
		t.Fatal("expected read-only file access rule rejection")
	}
}

func TestLocalWorkspaceFileAccessRuleNone(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		FileRules: []workspace.FileAccessRule{
			{Path: "*.secret", Access: workspace.FileAccessNone},
		},
	}))
	ctx := context.Background()
	if err := s.WriteFile(ctx, "db.secret", []byte("x")); err == nil {
		t.Fatal("expected none file access rule rejection")
	}
}

func TestLocalWorkspaceUnprotectedPathAllowsWrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalWorkspace(dir, WithSecurityPolicy(workspace.SecurityPolicy{
		ProtectedPaths: []string{".git"},
	}))
	ctx := context.Background()
	if err := s.WriteFile(ctx, "src/main.go", []byte("package main")); err != nil {
		t.Fatalf("expected write to succeed: %v", err)
	}
}
