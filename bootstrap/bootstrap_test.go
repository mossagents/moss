package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0700); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(agentsDir, "AGENTS.md"), []byte("Be helpful"), 0600)
	os.WriteFile(filepath.Join(agentsDir, "SOUL.md"), []byte("Friendly tone"), 0600)

	ctx := Load(dir)
	if ctx.Agents != "Be helpful" {
		t.Errorf("Agents = %q, want %q", ctx.Agents, "Be helpful")
	}
	if ctx.Soul != "Friendly tone" {
		t.Errorf("Soul = %q, want %q", ctx.Soul, "Friendly tone")
	}
	if ctx.Tools != "" {
		t.Errorf("Tools = %q, want empty", ctx.Tools)
	}
}

func TestEmpty(t *testing.T) {
	ctx := &Context{}
	if !ctx.Empty() {
		t.Error("empty context should report Empty() = true")
	}

	ctx.Identity = "test"
	if ctx.Empty() {
		t.Error("non-empty context should report Empty() = false")
	}
}

func TestSystemPromptSection(t *testing.T) {
	ctx := &Context{
		Identity: "I am mossclaw",
		Soul:     "Friendly",
	}

	section := ctx.SystemPromptSection()
	if section == "" {
		t.Fatal("expected non-empty section")
	}

	// Should contain XML tags
	if !contains(section, "<identity>") || !contains(section, "</identity>") {
		t.Error("missing identity tags")
	}
	if !contains(section, "<soul>") || !contains(section, "</soul>") {
		t.Error("missing soul tags")
	}
	// Should NOT contain empty tags
	if contains(section, "<agents>") {
		t.Error("should not contain empty agents tag")
	}
}

func TestPriority(t *testing.T) {
	dir := t.TempDir()

	// Create both .agents/ and .moss/ with different content
	agentsDir := filepath.Join(dir, ".agents")
	mossDir := filepath.Join(dir, ".moss")
	os.MkdirAll(agentsDir, 0700)
	os.MkdirAll(mossDir, 0700)

	os.WriteFile(filepath.Join(agentsDir, "AGENTS.md"), []byte("project-agents"), 0600)
	os.WriteFile(filepath.Join(mossDir, "AGENTS.md"), []byte("moss-agents"), 0600)
	os.WriteFile(filepath.Join(mossDir, "SOUL.md"), []byte("moss-soul"), 0600)

	ctx := Load(dir)

	// .agents/ should take priority over .moss/
	if ctx.Agents != "project-agents" {
		t.Errorf("Agents = %q, want %q (should prefer .agents/)", ctx.Agents, "project-agents")
	}
	// But files not in .agents/ should fall through to .moss/
	if ctx.Soul != "moss-soul" {
		t.Errorf("Soul = %q, want %q", ctx.Soul, "moss-soul")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
