package product

import (
	appconfig "github.com/mossagents/moss/config"
	intr "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/tool"
	"testing"
)

func TestPersistProjectApprovalAmendmentWritesProfileRule(t *testing.T) {
	workspace := t.TempDir()
	profile := "guarded"

	err := PersistProjectApprovalAmendment(workspace, profile, &intr.ExecPolicyAmendment{
		HTTPRule: &intr.ExecPolicyHTTPRule{
			Name:    "allow-api",
			Match:   "api.example.com",
			Methods: []string{"GET"},
		},
	})
	if err != nil {
		t.Fatalf("PersistProjectApprovalAmendment: %v", err)
	}

	cfg, err := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspace))
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

func TestEvaluatePolicy_ConfirmModeUsesEffectSemantics(t *testing.T) {
	rules, err := ApprovalModePolicyRules(ApprovalModeConfirm)
	if err != nil {
		t.Fatalf("ApprovalModePolicyRules: %v", err)
	}
	decision := EvaluatePolicy(rules, tool.ToolSpec{
		Name:         "write_memory",
		Risk:         tool.RiskLow,
		Capabilities: []string{"memory"},
	}, nil)
	if decision != builtins.RequireApproval {
		t.Fatalf("decision = %s, want %s", decision, builtins.RequireApproval)
	}
}

func TestEvaluatePolicy_ReadOnlyModeDeniesGraphMutation(t *testing.T) {
	rules, err := ApprovalModePolicyRules(ApprovalModeReadOnly)
	if err != nil {
		t.Fatalf("ApprovalModePolicyRules: %v", err)
	}
	decision := EvaluatePolicy(rules, tool.ToolSpec{
		Name:         "offload_context",
		Risk:         tool.RiskLow,
		Capabilities: []string{"context"},
	}, nil)
	if decision != builtins.Deny {
		t.Fatalf("decision = %s, want %s", decision, builtins.Deny)
	}
}
