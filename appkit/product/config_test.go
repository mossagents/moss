package product

import (
	"testing"

	appconfig "github.com/mossagents/moss/config"
)

func TestApplyConfigSetAndUnset(t *testing.T) {
	cfg := &appconfig.Config{}
	display, err := applyConfigSet(cfg, "provider", "openai", false)
	if err != nil {
		t.Fatalf("set provider: %v", err)
	}
	if display != "openai" {
		t.Fatalf("expected provider display openai, got %q", display)
	}
	if cfg.EffectiveAPIType() != "openai" {
		t.Fatalf("expected api_type=openai, got %q", cfg.EffectiveAPIType())
	}
	if _, err := applyConfigSet(cfg, "model", "gpt-5", false); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if cfg.Model != "gpt-5" {
		t.Fatalf("expected model gpt-5, got %q", cfg.Model)
	}
	if err := applyConfigUnset(cfg, "model", false); err != nil {
		t.Fatalf("unset model: %v", err)
	}
	if cfg.Model != "" {
		t.Fatalf("expected empty model, got %q", cfg.Model)
	}
}
