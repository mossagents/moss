package config

import "testing"

func TestNormalizeProviderIdentity(t *testing.T) {
	identity := NormalizeProviderIdentity("openai", "", "deepseek")
	if identity.APIType != "openai" {
		t.Fatalf("APIType = %q, want openai", identity.APIType)
	}
	if identity.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", identity.Provider)
	}
	if identity.Name != "deepseek" {
		t.Fatalf("Name = %q, want deepseek", identity.Name)
	}
	if identity.EffectiveAPIType() != "openai" {
		t.Fatalf("EffectiveAPIType = %q, want openai", identity.EffectiveAPIType())
	}
	if identity.DisplayName() != "deepseek" {
		t.Fatalf("DisplayName = %q, want deepseek", identity.DisplayName())
	}
	if identity.Label() != "deepseek (openai)" {
		t.Fatalf("Label = %q, want deepseek (openai)", identity.Label())
	}
}

func TestConfigProviderIdentityKeepsLegacyProviderCompatibility(t *testing.T) {
	cfg := &Config{Provider: "claude"}
	cfg.normalizeProviderFields()
	if cfg.APIType != "claude" {
		t.Fatalf("APIType = %q, want claude", cfg.APIType)
	}
	if cfg.Provider != "claude" {
		t.Fatalf("Provider = %q, want claude", cfg.Provider)
	}
	if cfg.Name != "claude" {
		t.Fatalf("Name = %q, want claude", cfg.Name)
	}
}
