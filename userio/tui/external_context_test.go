package tui

import (
	"encoding/base64"
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
	if strings.Contains(got, "Image reference attached by path") {
		t.Fatalf("unexpected image expansion: %q", got)
	}
}

func TestBuildUserContentPartsAddsInputImagePart(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "diagram.png")
	raw := []byte{0x89, 0x50, 0x4E, 0x47}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	parts, err := buildUserContentParts("Please inspect @diagram.png", workspace)
	if err != nil {
		t.Fatalf("buildUserContentParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text+image parts, got %d", len(parts))
	}
	if parts[1].Type != "input_image" {
		t.Fatalf("expected input_image part, got %q", parts[1].Type)
	}
	if parts[1].MIMEType != "image/png" {
		t.Fatalf("mime_type=%q", parts[1].MIMEType)
	}
	if parts[1].DataBase64 != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("unexpected base64 payload: %q", parts[1].DataBase64)
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
