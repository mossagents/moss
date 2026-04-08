package config

import (
	"testing"
)

func TestNormalizeProviderIdentity(t *testing.T) {
	identity := NormalizeProviderIdentity("openai", "", "deepseek")
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
	identity := NormalizeProviderIdentity("openai-responses", "", "")
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

func TestConfigProviderIdentityKeepsLegacyProviderCompatibility(t *testing.T) {
	cfg := &Config{Provider: "claude"}
	cfg.normalizeProviderFields()
	if cfg.Provider != "claude" {
		t.Fatalf("Provider = %q, want claude", cfg.Provider)
	}
	if cfg.Name != "claude" {
		t.Fatalf("Name = %q, want claude", cfg.Name)
	}
	if len(cfg.Models) != 1 || !cfg.Models[0].Default {
		t.Fatalf("expected legacy provider fields to synthesize one default model, got %+v", cfg.Models)
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
