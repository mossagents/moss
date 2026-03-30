package product

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/mossagents/moss/config"
)

func TestResolveRouterConfigPathPrefersExplicit(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.yaml")
	if got := ResolveRouterConfigPath(dir, explicit); got != explicit {
		t.Fatalf("ResolveRouterConfigPath explicit=%q, want %q", got, explicit)
	}
}

func TestResolveRouterConfigPathDiscoversWorkspaceConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mosscode", "models.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("models: []\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := ResolveRouterConfigPath(dir, ""); got != path {
		t.Fatalf("ResolveRouterConfigPath discovered=%q, want %q", got, path)
	}
}

func TestGovernanceConfigRetryAndBreaker(t *testing.T) {
	cfg := GovernanceConfig{
		LLMRetries:         3,
		LLMRetryInitial:    10 * time.Millisecond,
		LLMRetryMaxDelay:   50 * time.Millisecond,
		LLMBreakerFailures: 2,
		LLMBreakerReset:    5 * time.Second,
	}
	retryCfg, enabled := cfg.RetryConfig()
	if enabled == nil || !*enabled {
		t.Fatal("expected retry to be enabled")
	}
	if retryCfg == nil || retryCfg.MaxRetries != 3 {
		t.Fatalf("retryCfg=%+v, want MaxRetries=3", retryCfg)
	}
	breakerCfg := cfg.BreakerConfig()
	if breakerCfg == nil || breakerCfg.MaxFailures != 2 {
		t.Fatalf("breakerCfg=%+v, want MaxFailures=2", breakerCfg)
	}
}

func TestGovernanceConfigDisableRetry(t *testing.T) {
	retryCfg, enabled := (GovernanceConfig{LLMRetries: 0}).RetryConfig()
	if retryCfg != nil {
		t.Fatalf("expected nil retry config when disabled, got %+v", retryCfg)
	}
	if enabled == nil || *enabled {
		t.Fatalf("expected retry disabled marker, got %+v", enabled)
	}
}

func TestBuildGovernanceReportIncludesPricingCatalog(t *testing.T) {
	appconfig.SetAppName("moss-test")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".mosscode", "pricing.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("models:\n  gpt-5:\n    prompt_per_1m_usd: 1.0\n    completion_per_1m_usd: 2.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report := BuildGovernanceReport(dir, GovernanceConfig{})
	if report.PricingCatalog != path {
		t.Fatalf("pricing catalog=%q, want %q", report.PricingCatalog, path)
	}
	if report.PricingModels != 1 {
		t.Fatalf("pricing models=%d, want 1", report.PricingModels)
	}
}
