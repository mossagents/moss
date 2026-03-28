package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdownPreservesWindowsPathInCodeSpan(t *testing.T) {
	path := `C:\Users\qlind\.mosscode\config.yaml`
	out := renderMarkdown("Config file: `"+path+"`", 80)
	if !strings.Contains(out, path) {
		t.Fatalf("expected rendered markdown to preserve Windows path, got %q", out)
	}
}
