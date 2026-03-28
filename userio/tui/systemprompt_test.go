package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSystemPrompt_LoadsWorkspaceAgentsMarkdown(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("Be precise."), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	got := buildSystemPrompt(ws)
	if !strings.Contains(got, "## Bootstrap Context") {
		t.Fatalf("expected bootstrap context section, got: %s", got)
	}
	if !strings.Contains(got, "<agents>") || !strings.Contains(got, "Be precise.") {
		t.Fatalf("expected AGENTS.md content injected, got: %s", got)
	}
}
