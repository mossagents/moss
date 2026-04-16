package product

import (
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
)

func TestPersistProjectApprovalAmendmentWritesProfileRule(t *testing.T) {
	workspace := t.TempDir()
	profile := "guarded"

	err := PersistProjectApprovalAmendment(workspace, profile, &io.ExecPolicyAmendment{
		HTTPRule: &io.ExecPolicyHTTPRule{
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
	policy, err := approvalModeToolPolicyForTrust(appconfig.TrustTrusted, ApprovalModeConfirm)
	if err != nil {
		t.Fatalf("approvalModeToolPolicyForTrust: %v", err)
	}
	decision := EvaluateToolPolicy(policy, tool.ToolSpec{
		Name:         "write_memory",
		Risk:         tool.RiskLow,
		Capabilities: []string{"memory"},
	}, nil)
	if decision != governance.RequireApproval {
		t.Fatalf("decision = %s, want %s", decision, governance.RequireApproval)
	}
}

func TestEvaluatePolicy_ReadOnlyModeDeniesGraphMutation(t *testing.T) {
	policy, err := approvalModeToolPolicyForTrust(appconfig.TrustTrusted, ApprovalModeReadOnly)
	if err != nil {
		t.Fatalf("approvalModeToolPolicyForTrust: %v", err)
	}
	decision := EvaluateToolPolicy(policy, tool.ToolSpec{
		Name:         "offload_context",
		Risk:         tool.RiskLow,
		Capabilities: []string{"context"},
	}, nil)
	if decision != governance.Deny {
		t.Fatalf("decision = %s, want %s", decision, governance.Deny)
	}
}

func TestHasProjectCommandRule(t *testing.T) {
	rules := []appconfig.CommandRuleConfig{
		{Match: "git commit", Access: "allow"},
		{Match: "npm run *", Access: "allow"},
	}

	// Empty rules
	target := appconfig.CommandRuleConfig{Match: "git commit", Access: "allow"}
	if hasProjectCommandRule(nil, target) {
		t.Error("empty rules: expected false")
	}

	// Found — exact match
	if !hasProjectCommandRule(rules, target) {
		t.Error("expected true for existing rule")
	}

	// Found — case-insensitive match
	upper := appconfig.CommandRuleConfig{Match: "GIT COMMIT", Access: "ALLOW"}
	if !hasProjectCommandRule(rules, upper) {
		t.Error("expected true for case-insensitive match")
	}

	// Not found — different match
	diff := appconfig.CommandRuleConfig{Match: "make build", Access: "allow"}
	if hasProjectCommandRule(rules, diff) {
		t.Error("expected false for non-matching rule")
	}

	// Not found — same match but different access
	deny := appconfig.CommandRuleConfig{Match: "git commit", Access: "deny"}
	if hasProjectCommandRule(rules, deny) {
		t.Error("expected false when access differs")
	}
}
