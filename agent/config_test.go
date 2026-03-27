package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "researcher.yaml")

	content := `name: researcher
description: "Research assistant"
system_prompt: "You are a research expert."
tools:
  - grep
  - read_file
max_steps: 20
trust_level: restricted
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Name != "researcher" {
		t.Errorf("name = %q, want researcher", cfg.Name)
	}
	if cfg.Description != "Research assistant" {
		t.Errorf("description = %q", cfg.Description)
	}
	if cfg.SystemPrompt != "You are a research expert." {
		t.Errorf("system_prompt = %q", cfg.SystemPrompt)
	}
	if len(cfg.Tools) != 2 || cfg.Tools[0] != "grep" {
		t.Errorf("tools = %v", cfg.Tools)
	}
	if cfg.MaxSteps != 20 {
		t.Errorf("max_steps = %d", cfg.MaxSteps)
	}
	if cfg.TrustLevel != "restricted" {
		t.Errorf("trust_level = %q", cfg.TrustLevel)
	}
}

func TestParseConfigFile_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")

	content := `name: minimal
system_prompt: "Minimal agent."
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MaxSteps != 30 {
		t.Errorf("default max_steps = %d, want 30", cfg.MaxSteps)
	}
	if cfg.TrustLevel != "restricted" {
		t.Errorf("default trust_level = %q, want restricted", cfg.TrustLevel)
	}
}

func TestParseConfigFile_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	content := `system_prompt: "No name."
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseConfigFile(path)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadConfigsFromDir(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"researcher.yaml": `name: researcher
system_prompt: "Research."
tools: [grep]
`,
		"coder.yml": `name: coder
system_prompt: "Code."
tools: [write_file, read_file]
`,
		"readme.md": `# Not a config`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	configs, err := LoadConfigsFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(configs) != 2 {
		t.Fatalf("got %d configs, want 2", len(configs))
	}
}

func TestLoadConfigsFromDir_NonExistent(t *testing.T) {
	configs, err := LoadConfigsFromDir("/no/such/dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 0 {
		t.Fatalf("got %d configs, want 0", len(configs))
	}
}
