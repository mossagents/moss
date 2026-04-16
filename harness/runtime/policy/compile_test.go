package policy

import (
	"encoding/json"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel/tool"
)

func TestCompileRulesApplyCommandRules(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.Command.Rules = []CommandRule{{
		Name:   "git-push",
		Match:  "git push*",
		Access: ToolAccessRequireApproval,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	result := governance.Allow
	for _, rule := range rules {
		next := rule(governance.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == governance.Deny {
			result = governance.Deny
			break
		}
		if next.Decision == governance.RequireApproval {
			result = governance.RequireApproval
		}
	}
	if result != governance.RequireApproval {
		t.Fatalf("decision = %s, want %s", result, governance.RequireApproval)
	}
}

func TestCompileRulesAllowRuleOverridesDefaultConfirm(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "confirm")
	policy.Command.Rules = []CommandRule{{
		Name:   "git-status",
		Match:  "git status*",
		Access: ToolAccessAllow,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"status"},
	})
	result := governance.Allow
	for _, rule := range rules {
		next := rule(governance.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == governance.Deny {
			result = governance.Deny
			break
		}
		if next.Decision == governance.RequireApproval {
			result = governance.RequireApproval
		}
	}
	if result != governance.Allow {
		t.Fatalf("decision = %s, want %s", result, governance.Allow)
	}
}

// TestEvaluateDenyDangerousCommand 验证 Evaluate 对危险命令返回 Deny。
func TestEvaluateDenyDangerousCommand(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	input, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "run_command"}, input)
	if decision != governance.Deny {
		t.Fatalf("expected Deny for dangerous command, got %s", decision)
	}
}

// TestEvaluateProtectedPathPrefix 验证 Evaluate 对受保护路径返回 RequireApproval。
func TestEvaluateProtectedPathPrefix(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.ProtectedPathPrefixes = []string{"/etc/"}
	input, _ := json.Marshal(map[string]any{"path": "/etc/hosts"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "read_file"}, input)
	if decision != governance.RequireApproval {
		t.Fatalf("expected RequireApproval for protected path, got %s", decision)
	}
}

// TestEvaluateNormalToolAllowed 验证普通工具在全自动模式下直接放行。
func TestEvaluateNormalToolAllowed(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	decision := Evaluate(policy, tool.ToolSpec{Name: "list_files"}, nil)
	if decision != governance.Allow {
		t.Fatalf("expected Allow for normal tool in full-auto, got %s", decision)
	}
}

// TestEvaluateExplicitUserApprovalClass 验证 ApprovalClassExplicitUser 的工具需要审批。
func TestEvaluateExplicitUserApprovalClass(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	spec := tool.ToolSpec{
		Name:          "delete_account",
		ApprovalClass: tool.ApprovalClassExplicitUser,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != governance.RequireApproval {
		t.Fatalf("expected RequireApproval for ApprovalClassExplicitUser, got %s", decision)
	}
}

// TestEvaluateDenyApprovalClass 验证被拒绝的审批类返回 Deny。
func TestEvaluateDenyApprovalClass(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	spec := tool.ToolSpec{
		Name:          "privileged_op",
		ApprovalClass: tool.ApprovalClassSupervisorOnly,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != governance.Deny {
		t.Fatalf("expected Deny for denied approval class, got %s", decision)
	}
}
