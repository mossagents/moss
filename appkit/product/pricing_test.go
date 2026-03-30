package product

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

func TestResolvePricingCatalogPathPrefersExplicit(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "pricing.yaml")
	if got := ResolvePricingCatalogPath(dir, explicit); got != explicit {
		t.Fatalf("ResolvePricingCatalogPath explicit=%q, want %q", got, explicit)
	}
}

func TestResolvePricingCatalogPathDiscoversWorkspaceConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mosscode", "pricing.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("models: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := ResolvePricingCatalogPath(dir, ""); got != path {
		t.Fatalf("ResolvePricingCatalogPath discovered=%q, want %q", got, path)
	}
}

func TestOpenPricingCatalogAndEstimate(t *testing.T) {
	appconfig.SetAppName("moss-test")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	path := filepath.Join(t.TempDir(), "pricing.yaml")
	if err := os.WriteFile(path, []byte("models:\n  gpt-5:\n    prompt_per_1m_usd: 1.0\n    completion_per_1m_usd: 2.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	catalog, resolved, err := OpenPricingCatalog("", path)
	if err != nil {
		t.Fatalf("OpenPricingCatalog: %v", err)
	}
	if resolved != path {
		t.Fatalf("resolved=%q, want %q", resolved, path)
	}
	cost, ok := catalog.Estimate(port.TokenUsage{PromptTokens: 500000, CompletionTokens: 250000}, "gpt-5")
	if !ok {
		t.Fatal("expected cost match")
	}
	if cost != 1.0 {
		t.Fatalf("cost=%f, want 1.0", cost)
	}
}

func TestPricingObserverAnnotatesTraceRecorder(t *testing.T) {
	recorder := NewRunTraceRecorder()
	observer := NewPricingObserver(&PricingCatalog{
		Models: map[string]ModelPricing{
			"gpt-5": {PromptPer1MUSD: 1.0, CompletionPer1MUSD: 2.0},
		},
	}, recorder)
	observer.OnLLMCall(context.Background(), port.LLMCallEvent{
		SessionID: "sess-1",
		Model:     "gpt-5",
		Duration:  5 * time.Millisecond,
		Usage: port.TokenUsage{
			PromptTokens:     500000,
			CompletionTokens: 250000,
			TotalTokens:      750000,
		},
	})
	trace := recorder.Snapshot()
	if trace.TotalTokens != 750000 {
		t.Fatalf("total tokens=%d", trace.TotalTokens)
	}
	if trace.EstimatedCostUSD != 1.0 {
		t.Fatalf("estimated cost=%f, want 1.0", trace.EstimatedCostUSD)
	}
	if len(trace.Timeline) != 1 || trace.Timeline[0].Kind != "llm_call" {
		t.Fatalf("unexpected timeline %+v", trace.Timeline)
	}
}
