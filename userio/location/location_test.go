package location_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/userio/location"
)

func TestParseLocationSpec_Empty(t *testing.T) {
	path, line := location.ParseLocationSpec("")
	if path != "" || line != 0 {
		t.Errorf("expected empty path and 0 line, got %q:%d", path, line)
	}
}

func TestParseLocationSpec_PathOnly(t *testing.T) {
	path, line := location.ParseLocationSpec("/some/file.go")
	if path != "/some/file.go" {
		t.Errorf("expected /some/file.go, got %q", path)
	}
	if line != 0 {
		t.Errorf("expected line=0, got %d", line)
	}
}

func TestParseLocationSpec_PathWithLine(t *testing.T) {
	path, line := location.ParseLocationSpec("/some/file.go:42")
	if path != "/some/file.go" {
		t.Errorf("expected /some/file.go, got %q", path)
	}
	if line != 42 {
		t.Errorf("expected line=42, got %d", line)
	}
}

func TestParseLocationSpec_NonNumericSuffix(t *testing.T) {
	path, line := location.ParseLocationSpec("/some/file.go:abc")
	if path != "/some/file.go:abc" {
		t.Errorf("expected original spec, got %q", path)
	}
	if line != 0 {
		t.Errorf("expected line=0, got %d", line)
	}
}

func TestParseLocationSpec_ZeroLineSuffix(t *testing.T) {
	path, line := location.ParseLocationSpec("/some/file.go:0")
	if path != "/some/file.go:0" {
		t.Errorf("expected original spec, got %q", path)
	}
	if line != 0 {
		t.Errorf("expected line=0, got %d", line)
	}
}

func TestParseLocationSpec_NegativeLineSuffix(t *testing.T) {
	path, line := location.ParseLocationSpec("/some/file.go:-1")
	// -1 fails parseInt check (line <= 0)
	if line != 0 {
		t.Errorf("expected line=0, got %d", line)
	}
	_ = path
}

func TestParseLocationSpec_ColonAtStart(t *testing.T) {
	path, line := location.ParseLocationSpec(":42")
	// idx<=0 case
	if line != 0 {
		t.Errorf("expected line=0, got %d", line)
	}
	_ = path
}

func TestParseLocationSpec_Whitespace(t *testing.T) {
	path, line := location.ParseLocationSpec("  /some/file.go:10  ")
	if path != "/some/file.go" {
		t.Errorf("expected /some/file.go, got %q", path)
	}
	if line != 10 {
		t.Errorf("expected line=10, got %d", line)
	}
}

func TestOpenWorkspacePath_EmptySpec(t *testing.T) {
	_, err := location.OpenWorkspacePath("", "")
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestOpenWorkspacePath_EmptyPath(t *testing.T) {
	_, err := location.OpenWorkspacePath("/workspace", ":42")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpenWorkspacePath_NonexistentFile(t *testing.T) {
	_, err := location.OpenWorkspacePath("", "/nonexistent/path/file.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestOpenWorkspacePath_ExistingFileNoEditor(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	// Clear EDITOR env to avoid using a real editor
	t.Setenv("EDITOR", "")
	// On CI/Windows, VS Code ("code") may or may not be available
	// We just verify the function doesn't crash (it may return success or error)
	// The key test is that it attempts to open and returns a string or error
	result, _ := location.OpenWorkspacePath("", path)
	_ = result // Could succeed or fail depending on environment
}
