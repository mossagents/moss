package attachments_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/harness/userio/attachments"
)

// minPNG is a 1×1 transparent PNG (67 bytes).
var minPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
	0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func writeTmpFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writeTmpFile: %v", err)
	}
	return path
}

// ——— IsMediaPath ———

func TestIsMediaPath_Image(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if !attachments.IsMediaPath("file" + ext) {
			t.Errorf("expected IsMediaPath=true for %s", ext)
		}
	}
}

func TestIsMediaPath_Audio(t *testing.T) {
	for _, ext := range []string{".wav", ".mp3", ".mpeg", ".m4a", ".ogg", ".flac"} {
		if !attachments.IsMediaPath("file" + ext) {
			t.Errorf("expected IsMediaPath=true for %s", ext)
		}
	}
}

func TestIsMediaPath_Video(t *testing.T) {
	for _, ext := range []string{".mp4", ".webm", ".mov", ".avi", ".mkv"} {
		if !attachments.IsMediaPath("file" + ext) {
			t.Errorf("expected IsMediaPath=true for %s", ext)
		}
	}
}

func TestIsMediaPath_NonMedia(t *testing.T) {
	for _, ext := range []string{".txt", ".go", ".json", ".pdf", ""} {
		if attachments.IsMediaPath("file" + ext) {
			t.Errorf("expected IsMediaPath=false for %q", ext)
		}
	}
}

func TestIsMediaPath_CaseInsensitive(t *testing.T) {
	if !attachments.IsMediaPath("photo.PNG") {
		t.Error("expected IsMediaPath=true for .PNG")
	}
	if !attachments.IsMediaPath("video.MP4") {
		t.Error("expected IsMediaPath=true for .MP4")
	}
}

// ——— ResolveMentionPath ———

func TestResolveMentionPath_Empty(t *testing.T) {
	_, ok := attachments.ResolveMentionPath("", "")
	if ok {
		t.Fatal("expected false for empty raw")
	}
}

func TestResolveMentionPath_AbsolutePath(t *testing.T) {
	path := writeTmpFile(t, "hello.txt", []byte("hi"))
	got, ok := attachments.ResolveMentionPath("", path)
	if !ok {
		t.Fatal("expected true for existing absolute path")
	}
	if !strings.HasSuffix(got, "hello.txt") {
		t.Errorf("unexpected resolved path: %s", got)
	}
}

func TestResolveMentionPath_RelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rel.txt")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := attachments.ResolveMentionPath(dir, "rel.txt")
	if !ok {
		t.Fatal("expected true for file relative to workspace")
	}
	if !strings.HasSuffix(got, "rel.txt") {
		t.Errorf("unexpected resolved path: %s", got)
	}
}

func TestResolveMentionPath_NotFound(t *testing.T) {
	_, ok := attachments.ResolveMentionPath("", "/does/not/exist/file.txt")
	if ok {
		t.Fatal("expected false for non-existent path")
	}
}

func TestResolveMentionPath_Directory(t *testing.T) {
	dir := t.TempDir()
	_, ok := attachments.ResolveMentionPath("", dir)
	if ok {
		t.Fatal("expected false for a directory path")
	}
}

func TestResolveMentionPath_StripsDecorators(t *testing.T) {
	path := writeTmpFile(t, "strip.txt", []byte("x"))
	quoted := `"` + path + `",`
	got, ok := attachments.ResolveMentionPath("", quoted)
	if !ok {
		t.Fatalf("expected true, got false (quoted=%q)", quoted)
	}
	if !strings.HasSuffix(got, "strip.txt") {
		t.Errorf("unexpected path: %s", got)
	}
}

// ——— DetectMediaPart ———

func TestDetectMediaPart_PNG(t *testing.T) {
	partType, mimeType, err := attachments.DetectMediaPart("photo.png", minPNG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mimeType != "image/png" {
		t.Errorf("unexpected mime: %s", mimeType)
	}
	_ = partType
}

func TestDetectMediaPart_UnsupportedExtension(t *testing.T) {
	_, _, err := attachments.DetectMediaPart("file.bin", []byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for unsupported media type")
	}
}

func TestDetectMediaPart_ExtFamilyMismatch(t *testing.T) {
	// .jpg extension but the bytes are not an image
	data := []byte("plain text content here which is not an image")
	_, _, err := attachments.DetectMediaPart("photo.jpg", data)
	// should either succeed (fallback) or error depending on sniff result
	_ = err
}

// ——— ExpandInlineFileMentions ———

func TestExpandInlineFileMentions_PassThrough(t *testing.T) {
	input := "hello world"
	got, err := attachments.ExpandInlineFileMentions(input, "/workspace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

// ——— BuildUserContentParts ———

func TestBuildUserContentParts_NoMentions(t *testing.T) {
	parts, err := attachments.BuildUserContentParts("hello world", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
}

func TestBuildUserContentParts_EmptyInput(t *testing.T) {
	parts, err := attachments.BuildUserContentParts("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 0 {
		t.Errorf("expected 0 parts for empty input, got %d", len(parts))
	}
}

func TestBuildUserContentParts_MissingMentionFile(t *testing.T) {
	_, err := attachments.BuildUserContentParts("check @/nonexistent/file.txt", "")
	if err == nil {
		t.Fatal("expected error for missing mention file")
	}
}

func TestBuildUserContentParts_WithFileAttachment(t *testing.T) {
	path := writeTmpFile(t, "note.txt", []byte("content"))
	input := "see @" + path
	parts, err := attachments.BuildUserContentParts(input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// text part + file ref part
	if len(parts) < 2 {
		t.Errorf("expected >=2 parts, got %d", len(parts))
	}
}

// ——— BuildComposerSubmission ———

func TestBuildComposerSubmission_NoAttachments(t *testing.T) {
	display, runText, parts, err := attachments.BuildComposerSubmission("hello", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if display != "hello" {
		t.Errorf("unexpected display: %q", display)
	}
	if runText != "hello" {
		t.Errorf("unexpected runText: %q", runText)
	}
	if len(parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(parts))
	}
}

func TestBuildComposerSubmission_WithPendingAttachment(t *testing.T) {
	path := writeTmpFile(t, "doc.txt", []byte("data"))
	pending := []attachments.ComposerAttachment{
		{
			Key:   "dockey",
			Label: "doc.txt",
			Path:  path,
			Kind:  "file",
		},
	}
	_, _, parts, err := attachments.BuildComposerSubmission("see doc", "", pending)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// text part + file ref part
	if len(parts) < 2 {
		t.Errorf("expected >=2 parts, got %d", len(parts))
	}
}

func TestBuildComposerSubmission_DedupMentions(t *testing.T) {
	path := writeTmpFile(t, "dup.txt", []byte("x"))
	// Mention the same file twice
	input := "look @" + path + " and @" + path
	_, _, parts, err := attachments.BuildComposerSubmission(input, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// text part + only 1 deduped file part
	if len(parts) != 2 {
		t.Errorf("expected 2 parts (text+1 file), got %d", len(parts))
	}
}

func TestBuildComposerSubmission_DisplayIncludesAttachments(t *testing.T) {
	path := writeTmpFile(t, "readme.txt", []byte("hello"))
	input := "check @" + path
	display, _, _, err := attachments.BuildComposerSubmission(input, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(display, "Attachments:") {
		t.Errorf("expected display to include Attachments section, got: %q", display)
	}
}

// ——— BuildAttachmentDraft ———

func TestBuildAttachmentDraft_NotFound(t *testing.T) {
	_, err := attachments.BuildAttachmentDraft("", "/no/such/file.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestBuildAttachmentDraft_Found(t *testing.T) {
	path := writeTmpFile(t, "draft.txt", []byte("hi"))
	att, err := attachments.BuildAttachmentDraft("", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if att.Kind != "file" {
		t.Errorf("expected kind=file, got %q", att.Kind)
	}
	if att.Label != "draft.txt" {
		t.Errorf("unexpected label: %q", att.Label)
	}
}

func TestBuildAttachmentDraft_Image(t *testing.T) {
	path := writeTmpFile(t, "img.png", minPNG)
	att, err := attachments.BuildAttachmentDraft("", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if att.Kind != "image" {
		t.Errorf("expected kind=image, got %q", att.Kind)
	}
}
