package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/kernel"
	kt "github.com/mossagents/moss/harness/testing"
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

func TestSubagentCatalogValueInstallsAgentDelegationSubstrate(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(kt.NewRecorderIO()),
	)
	h := New(k, nil)
	reg := NewSubagentCatalog()
	if err := reg.Register(SubagentConfig{
		Name:         "reviewer",
		Description:  "Review delegated output",
		SystemPrompt: "Review the delegated result carefully.",
		Tools:        []string{"read_file"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := h.Install(context.Background(), SubagentCatalogValue(reg)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, ok := SubagentCatalogOf(k).Get("reviewer"); !ok {
		t.Fatal("expected installed subagent catalog to be readable")
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("delegate_agent"); !ok {
		t.Fatal("expected delegation tools after boot")
	}
}

func TestSubagentCatalog_List(t *testing.T) {
	cat := NewSubagentCatalog()
	_ = cat.Register(SubagentConfig{Name: "agent-a", Description: "A", SystemPrompt: "Do A"})
	_ = cat.Register(SubagentConfig{Name: "agent-b", Description: "B", SystemPrompt: "Do B"})

	list := cat.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 subagents, got %d", len(list))
	}
	names := map[string]bool{}
	for _, c := range list {
		names[c.Name] = true
	}
	if !names["agent-a"] || !names["agent-b"] {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestSubagentCatalog_NilHandling(t *testing.T) {
	var cat *SubagentCatalog
	if cat.List() != nil {
		t.Fatal("nil catalog List should return nil")
	}
	if _, ok := cat.Get("x"); ok {
		t.Fatal("nil catalog Get should return false")
	}
	if err := cat.Register(SubagentConfig{Name: "x"}); err == nil {
		t.Fatal("nil catalog Register should return error")
	}
}

func TestRegisterSubagent_Idempotent(t *testing.T) {
	k := kernel.New()
	cfg := SubagentConfig{Name: "deduped", Description: "test", SystemPrompt: "sp"}
	if err := RegisterSubagent(k, cfg); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Second registration of same name should be a no-op
	if err := RegisterSubagent(k, cfg); err != nil {
		t.Fatalf("second register should be idempotent: %v", err)
	}
	list := SubagentCatalogOf(k).List()
	count := 0
	for _, c := range list {
		if c.Name == "deduped" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 deduped subagent, got %d", count)
	}
}
