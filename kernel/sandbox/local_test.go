package sandbox

import (
	"context"
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
		out, err = s.Execute(context.Background(), "cmd", []string{"/C", "echo", "hello"})
	} else {
		out, err = s.Execute(context.Background(), "echo", []string{"hello"})
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

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)

	files, err := s.ListFiles("*.txt")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListFiles len = %d, want 2", len(files))
	}
}
