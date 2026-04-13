package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/kernel"
)

func TestRegisterSubagentUsesHarnessCatalog(t *testing.T) {
	k := kernel.New()

	if err := RegisterSubagent(k, SubagentConfig{
		Name:         "reviewer",
		Description:  "Review delegated output",
		SystemPrompt: "Review the delegated result carefully.",
		Tools:        []string{"read_file"},
	}); err != nil {
		t.Fatalf("RegisterSubagent: %v", err)
	}

	got, ok := SubagentCatalogOf(k).Get("reviewer")
	if !ok {
		t.Fatal("expected subagent to be registered")
	}
	if got.Name != "reviewer" {
		t.Fatalf("subagent name = %q, want reviewer", got.Name)
	}
	if got.MaxSteps <= 0 {
		t.Fatalf("expected default max_steps to be set, got %d", got.MaxSteps)
	}
}

func TestLoadSubagentsFromYAMLUsesHarnessCatalog(t *testing.T) {
	k := kernel.New()
	path := filepath.Join(t.TempDir(), "subagents.yaml")
	data := []byte(`
researcher:
  description: Research helper
  system_prompt: |
    Gather evidence and summarize the findings.
  tools:
    - web_fetch
  max_steps: 12
  trust_level: trusted
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := LoadSubagentsFromYAML(k, path); err != nil {
		t.Fatalf("LoadSubagentsFromYAML: %v", err)
	}

	got, ok := SubagentCatalogOf(k).Get("researcher")
	if !ok {
		t.Fatal("expected researcher subagent to be loaded")
	}
	if got.TrustLevel != "trusted" {
		t.Fatalf("trust level = %q, want trusted", got.TrustLevel)
	}
	if got.MaxSteps != 12 {
		t.Fatalf("max_steps = %d, want 12", got.MaxSteps)
	}
}
