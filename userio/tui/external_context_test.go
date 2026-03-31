package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandInlineFileMentionsAddsAttachedContext(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	got, err := expandInlineFileMentions("Please inspect @note.txt", workspace)
	if err != nil {
		t.Fatalf("expandInlineFileMentions: %v", err)
	}
	if !strings.Contains(got, "Attached context:") || !strings.Contains(got, "hello world") {
		t.Fatalf("unexpected expanded text: %q", got)
	}
}

func TestExpandInlineFileMentionsAddsImageReference(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	got, err := expandInlineFileMentions("Please inspect @diagram.png", workspace)
	if err != nil {
		t.Fatalf("expandInlineFileMentions: %v", err)
	}
	if !strings.Contains(got, "Image reference attached by path") {
		t.Fatalf("unexpected image expansion: %q", got)
	}
}

func TestParseLocationSpecExtractsLine(t *testing.T) {
	path, line := parseLocationSpec("userio\\tui\\chat.go:42")
	if path != "userio\\tui\\chat.go" || line != 42 {
		t.Fatalf("unexpected location parse: path=%q line=%d", path, line)
	}
}

func TestOpenWorkspacePathReturnsErrorForMissingTarget(t *testing.T) {
	_, err := openWorkspacePath(t.TempDir(), "missing.txt")
	if err == nil {
		t.Fatal("expected missing file error")
	}
}
