package product

import (
	"path/filepath"
	"testing"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

func TestPersistProjectApprovalAmendmentWritesProfileRule(t *testing.T) {
	workspace := t.TempDir()
	profile := "guarded"

	err := PersistProjectApprovalAmendment(workspace, profile, &port.ExecPolicyAmendment{
		HTTPRule: &port.ExecPolicyHTTPRule{
			Name:    "allow-api",
			Match:   "api.example.com",
			Methods: []string{"GET"},
		},
	})
	if err != nil {
		t.Fatalf("PersistProjectApprovalAmendment: %v", err)
	}

	cfg, err := appconfig.LoadConfig(filepath.Join(workspace, "moss.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	profileCfg, ok := cfg.Profiles[profile]
	if !ok {
		t.Fatalf("expected profile %q to be written", profile)
	}
	if len(profileCfg.Execution.HTTPRules) != 1 {
		t.Fatalf("http rules = %d, want 1", len(profileCfg.Execution.HTTPRules))
	}
	rule := profileCfg.Execution.HTTPRules[0]
	if rule.Match != "api.example.com" || rule.Access != "allow" {
		t.Fatalf("unexpected rule: %+v", rule)
	}
}
