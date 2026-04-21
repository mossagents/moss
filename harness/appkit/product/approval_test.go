package product

import (
	"encoding/json"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/harness/runtime/permissions"
	runtimepolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

func TestPersistProjectApprovalAmendmentWritesProfileRule(t *testing.T) {
	workspace := t.TempDir()

	err := PersistProjectApprovalAmendment(workspace, &io.ExecPolicyAmendment{
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
	profileCfg, ok := cfg.Profiles["default"]
	if !ok {
		t.Fatalf("expected profile %q to be written", "default")
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

func TestApplySessionConfigUsesResolvedSessionPolicy(t *testing.T) {
	compiled, err := permissions.Compile(permissions.Profile{
		Name:           "legacy:readonly",
		ApprovalPolicy: ApprovalModeReadOnly,
	}, appconfig.TrustRestricted)
	if err != nil {
		t.Fatalf("permissions.Compile: %v", err)
	}
	policyJSON, err := json.Marshal(compiled)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	k := kernel.New()
	err = ApplySessionConfig(k, session.SessionConfig{
		TrustLevel: appconfig.TrustRestricted,
		ResolvedSessionSpec: &session.ResolvedSessionSpec{
			Runtime: session.ResolvedRuntime{PermissionPolicy: policyJSON},
		},
	})
	if err != nil {
		t.Fatalf("ApplySessionConfig: %v", err)
	}
	policy, ok := runtimepolicy.Current(k)
	if !ok {
		t.Fatal("expected tool policy to be installed")
	}
	if policy.ApprovalMode != ApprovalModeReadOnly {
		t.Fatalf("policy.ApprovalMode = %q, want %q", policy.ApprovalMode, ApprovalModeReadOnly)
	}
	if policy.Trust != appconfig.TrustRestricted {
		t.Fatalf("policy.Trust = %q, want %q", policy.Trust, appconfig.TrustRestricted)
	}
}

func TestApplySessionConfigFallsBackToMetadataPolicy(t *testing.T) {
	policyMeta, err := runtimepolicy.EncodeToolPolicyMetadata(runtimepolicy.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, ApprovalModeConfirm))
	if err != nil {
		t.Fatalf("EncodeToolPolicyMetadata: %v", err)
	}
	k := kernel.New()
	err = ApplySessionConfig(k, session.SessionConfig{
		Metadata: map[string]any{session.MetadataToolPolicy: policyMeta},
	})
	if err != nil {
		t.Fatalf("ApplySessionConfig: %v", err)
	}
	policy, ok := runtimepolicy.Current(k)
	if !ok {
		t.Fatal("expected tool policy to be installed")
	}
	if policy.ApprovalMode != ApprovalModeConfirm {
		t.Fatalf("policy.ApprovalMode = %q, want %q", policy.ApprovalMode, ApprovalModeConfirm)
	}
}
