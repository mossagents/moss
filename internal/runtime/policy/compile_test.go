package policy

import (
	"encoding/json"
	"testing"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/runtime"
)

func TestCompileRulesApplyCommandRules(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.Command.Rules = []runtime.CommandRule{{
		Name:   "git-push",
		Match:  "git push*",
		Access: runtime.ToolAccessRequireApproval,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	result := builtins.Allow
	for _, rule := range rules {
		next := rule(builtins.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == builtins.Deny {
			result = builtins.Deny
			break
		}
		if next.Decision == builtins.RequireApproval {
			result = builtins.RequireApproval
		}
	}
	if result != builtins.RequireApproval {
		t.Fatalf("decision = %s, want %s", result, builtins.RequireApproval)
	}
}

func TestCompileRulesAllowRuleOverridesDefaultConfirm(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "confirm")
	policy.Command.Rules = []runtime.CommandRule{{
		Name:   "git-status",
		Match:  "git status*",
		Access: runtime.ToolAccessAllow,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"status"},
	})
	result := builtins.Allow
	for _, rule := range rules {
		next := rule(builtins.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == builtins.Deny {
			result = builtins.Deny
			break
		}
		if next.Decision == builtins.RequireApproval {
			result = builtins.RequireApproval
		}
	}
	if result != builtins.Allow {
		t.Fatalf("decision = %s, want %s", result, builtins.Allow)
	}
}

// TestEvaluateDenyDangerousCommand 验证 Evaluate 对危险命令返回 Deny。
func TestEvaluateDenyDangerousCommand(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	input, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "run_command"}, input)
	if decision != builtins.Deny {
		t.Fatalf("expected Deny for dangerous command, got %s", decision)
	}
}

// TestEvaluateProtectedPathPrefix 验证 Evaluate 对受保护路径返回 RequireApproval。
func TestEvaluateProtectedPathPrefix(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.ProtectedPathPrefixes = []string{"/etc/"}
	input, _ := json.Marshal(map[string]any{"path": "/etc/hosts"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "read_file"}, input)
	if decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval for protected path, got %s", decision)
	}
}

// TestEvaluateNormalToolAllowed 验证普通工具在全自动模式下直接放行。
func TestEvaluateNormalToolAllowed(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	decision := Evaluate(policy, tool.ToolSpec{Name: "list_files"}, nil)
	if decision != builtins.Allow {
		t.Fatalf("expected Allow for normal tool in full-auto, got %s", decision)
	}
}

// TestEvaluateExplicitUserApprovalClass 验证 ApprovalClassExplicitUser 的工具需要审批。
func TestEvaluateExplicitUserApprovalClass(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	spec := tool.ToolSpec{
		Name:          "delete_account",
		ApprovalClass: tool.ApprovalClassExplicitUser,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval for ApprovalClassExplicitUser, got %s", decision)
	}
}

// TestEvaluateDenyApprovalClass 验证被拒绝的审批类返回 Deny。
func TestEvaluateDenyApprovalClass(t *testing.T) {
	policy := runtime.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	spec := tool.ToolSpec{
		Name:          "privileged_op",
		ApprovalClass: tool.ApprovalClassSupervisorOnly,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != builtins.Deny {
		t.Fatalf("expected Deny for denied approval class, got %s", decision)
	}
}
