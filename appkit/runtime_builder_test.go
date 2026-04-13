package appkit

import "testing"

func TestResolveRuntimeFeatureFlags_DefaultDisabled(t *testing.T) {
	flags := &AppFlags{}
	flags.ApplyDefaults()
	got := resolveRuntimeFeatureFlags(flags)
	if got.EnableSummarize {
		t.Fatal("expected summarize disabled by default")
	}
	if got.EnableRAG {
		t.Fatal("expected rag disabled by default")
	}
	if got.BudgetGovernance != "observe-only" {
		t.Fatalf("expected observe-only budget governance by default, got %q", got.BudgetGovernance)
	}
}

func TestResolveRuntimeFeatureFlags_UsesFlags(t *testing.T) {
	got := resolveRuntimeFeatureFlags(&AppFlags{
		EnableSummarize:  true,
		EnableRAG:        true,
		PromptVersion:    "p1-unified-v1",
		BudgetGovernance: "enforce",
		GlobalMaxTokens:  9000,
		GlobalMaxSteps:   90,
		GlobalWarnAt:     0.8,
	})
	if !got.EnableSummarize {
		t.Fatal("expected summarize enabled")
	}
	if !got.EnableRAG {
		t.Fatal("expected rag enabled")
	}
	if got.PromptVersion != "p1-unified-v1" {
		t.Fatalf("expected prompt version override, got %q", got.PromptVersion)
	}
	if got.BudgetGovernance != "enforce" {
		t.Fatalf("expected enforce policy, got %q", got.BudgetGovernance)
	}
	if got.GlobalMaxTokens != 9000 || got.GlobalMaxSteps != 90 {
		t.Fatalf("unexpected global limits: tokens=%d steps=%d", got.GlobalMaxTokens, got.GlobalMaxSteps)
	}
}
