package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, TrustLevelTrusted)

	resolved, err := m.ResolvePath("foo.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(dir, "foo.txt")
	if resolved != expected {
		t.Errorf("expected %s, got %s", expected, resolved)
	}
}

func TestResolvePathTraversal(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, TrustLevelTrusted)

	_, err := m.ResolvePath("../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestWriteAndReadFile(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, TrustLevelTrusted)

	content := "hello world"
	if err := m.WriteFile("test.txt", content); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	got, err := m.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, TrustLevelTrusted)

	// Create some test files
	files := []string{"a.txt", "b.txt", "c.go"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := m.ListFiles("*.txt")
	if err != nil {
		t.Fatalf("ListFiles error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}
