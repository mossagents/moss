package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	userattachments "github.com/mossagents/moss/userio/attachments"
	userlocation "github.com/mossagents/moss/userio/location"
)

func TestExpandInlineFileMentionsLeavesTextUntouched(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	got, err := userattachments.ExpandInlineFileMentions("Please inspect @note.txt", workspace)
	if err != nil {
		t.Fatalf("expandInlineFileMentions: %v", err)
	}
	if got != "Please inspect @note.txt" {
		t.Fatalf("unexpected expanded text: %q", got)
	}
}

func TestExpandInlineFileMentionsAddsImageReference(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	got, err := userattachments.ExpandInlineFileMentions("Please inspect @diagram.png", workspace)
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
	parts, err := userattachments.BuildUserContentParts("Please inspect @diagram.png", workspace)
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

func TestBuildUserContentPartsAddsFileReferencePart(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	parts, err := userattachments.BuildUserContentParts("Please inspect @note.txt", workspace)
	if err != nil {
		t.Fatalf("buildUserContentParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text+file parts, got %d", len(parts))
	}
	if parts[1].Type != "file_ref" {
		t.Fatalf("expected file_ref part, got %q", parts[1].Type)
	}
	if parts[1].Attachment == nil || !strings.HasSuffix(parts[1].Attachment.Path, "note.txt") {
		t.Fatalf("unexpected attachment payload: %#v", parts[1].Attachment)
	}
}

func TestBuildUserContentPartsAddsInputAudioPart(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "voice.wav")
	raw := []byte("RIFF....WAVEfmt ")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	parts, err := userattachments.BuildUserContentParts("Please inspect @voice.wav", workspace)
	if err != nil {
		t.Fatalf("buildUserContentParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text+audio parts, got %d", len(parts))
	}
	if parts[1].Type != "input_audio" {
		t.Fatalf("expected input_audio part, got %q", parts[1].Type)
	}
}

func TestBuildUserContentPartsAddsInputVideoPart(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "clip.mp4")
	raw := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write video: %v", err)
	}
	parts, err := userattachments.BuildUserContentParts("Please inspect @clip.mp4", workspace)
	if err != nil {
		t.Fatalf("buildUserContentParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text+video parts, got %d", len(parts))
	}
	if parts[1].Type != "input_video" {
		t.Fatalf("expected input_video part, got %q", parts[1].Type)
	}
}

func TestParseLocationSpecExtractsLine(t *testing.T) {
	path, line := userlocation.ParseLocationSpec("userio\\tui\\chat.go:42")
	if path != "userio\\tui\\chat.go" || line != 42 {
		t.Fatalf("unexpected location parse: path=%q line=%d", path, line)
	}
}

func TestOpenWorkspacePathReturnsErrorForMissingTarget(t *testing.T) {
	_, err := userlocation.OpenWorkspacePath(t.TempDir(), "missing.txt")
	if err == nil {
		t.Fatal("expected missing file error")
	}
}
