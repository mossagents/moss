package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeProviderIdentity(t *testing.T) {
	identity := NormalizeProviderIdentity("openai", "deepseek")
	if identity.APIType != APITypeOpenAICompletions {
		t.Fatalf("APIType = %q, want %s", identity.APIType, APITypeOpenAICompletions)
	}
	if identity.Provider != APITypeOpenAICompletions {
		t.Fatalf("Provider = %q, want %s", identity.Provider, APITypeOpenAICompletions)
	}
	if identity.Name != "deepseek" {
		t.Fatalf("Name = %q, want deepseek", identity.Name)
	}
	if identity.EffectiveAPIType() != APITypeOpenAICompletions {
		t.Fatalf("EffectiveAPIType = %q, want %s", identity.EffectiveAPIType(), APITypeOpenAICompletions)
	}
	if identity.DisplayName() != "deepseek" {
		t.Fatalf("DisplayName = %q, want deepseek", identity.DisplayName())
	}
	if identity.Label() != "deepseek (openai-completions)" {
		t.Fatalf("Label = %q, want deepseek (openai-completions)", identity.Label())
	}
}

func TestNormalizeProviderIdentity_OpenAIResponses(t *testing.T) {
	identity := NormalizeProviderIdentity("openai-responses", "")
	if identity.APIType != APITypeOpenAIResponses {
		t.Fatalf("APIType = %q, want %s", identity.APIType, APITypeOpenAIResponses)
	}
	if identity.Provider != APITypeOpenAIResponses {
		t.Fatalf("Provider = %q, want %s", identity.Provider, APITypeOpenAIResponses)
	}
	if identity.Name != APITypeOpenAIResponses {
		t.Fatalf("Name = %q, want %s", identity.Name, APITypeOpenAIResponses)
	}
}

func TestConfigProviderIdentitySynthesizesDefaultModelFromTopLevelFields(t *testing.T) {
	cfg := &Config{Provider: "claude"}
	cfg.normalizeProviderFields()
	if cfg.Provider != "claude" {
		t.Fatalf("Provider = %q, want claude", cfg.Provider)
	}
	if cfg.Name != "claude" {
		t.Fatalf("Name = %q, want claude", cfg.Name)
	}
	if len(cfg.Models) != 1 || !cfg.Models[0].Default {
		t.Fatalf("expected top-level provider fields to synthesize one default model, got %+v", cfg.Models)
	}
}

func TestSkillConfigIsRequiredDefaultFalse(t *testing.T) {
	var sc SkillConfig
	if sc.IsRequired() {
		t.Fatal("IsRequired should be false by default")
	}
}

func TestSkillConfigIsRequiredHonorsValue(t *testing.T) {
	vTrue := true
	vFalse := false
	if !(SkillConfig{Required: &vTrue}).IsRequired() {
		t.Fatal("IsRequired should be true when required=true")
	}
	if (SkillConfig{Required: &vFalse}).IsRequired() {
		t.Fatal("IsRequired should be false when required=false")
	}
}

func TestDefaultProjectConfigPathUsesDotMossDir(t *testing.T) {
	workspace := filepath.Join("C:\\", "workspace")
	got := DefaultProjectConfigPath(workspace)
	want := filepath.Join(workspace, ".moss", "config.yaml")
	if got != want {
		t.Fatalf("DefaultProjectConfigPath() = %q, want %q", got, want)
	}
}

func TestSaveAndLoadProjectConfigUsesDotMossDir(t *testing.T) {
	workspace := t.TempDir()
	cfg := &Config{DefaultProfile: "guarded"}
	path := DefaultProjectConfigPath(workspace)

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".moss")); err != nil {
		t.Fatalf("Stat(.moss) = %v", err)
	}

	loaded, err := LoadProjectConfig(workspace)
	if err != nil {
		t.Fatalf("LoadProjectConfig() = %v", err)
	}
	if loaded.DefaultProfile != "guarded" {
		t.Fatalf("DefaultProfile = %q, want guarded", loaded.DefaultProfile)
	}
}

func TestLoadProjectConfigIgnoresLegacyRootMossYAML(t *testing.T) {
	workspace := t.TempDir()
	legacyPath := filepath.Join(workspace, "moss.yaml")
	if err := os.WriteFile(legacyPath, []byte("default_profile: legacy\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) = %v", legacyPath, err)
	}

	loaded, err := LoadProjectConfig(workspace)
	if err != nil {
		t.Fatalf("LoadProjectConfig() = %v", err)
	}
	if loaded.DefaultProfile != "" {
		t.Fatalf("DefaultProfile = %q, want empty", loaded.DefaultProfile)
	}
}
