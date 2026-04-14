package runtimepolicy

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
