package product

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/tool"
)

const (
	ApprovalModeReadOnly = "read-only"
	ApprovalModeConfirm  = "confirm"
	ApprovalModeFullAuto = "full-auto"
)

var (
	confirmModeTools = []string{
		"write_file", "edit_file", "run_command", "http_request",
		"spawn_agent", "task", "cancel_task", "update_task",
		"write_memory", "delete_memory", "offload_context",
		"acquire_workspace", "release_workspace",
	}
	readOnlyDeniedTools = []string{
		"write_file", "edit_file", "run_command", "http_request",
		"spawn_agent", "task", "cancel_task", "update_task",
		"write_memory", "delete_memory", "offload_context",
		"acquire_workspace", "release_workspace",
	}
)

func NormalizeApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "confirm", "ask", "safe":
		return ApprovalModeConfirm
	case "read-only", "readonly", "ro":
		return ApprovalModeReadOnly
	case "full-auto", "full", "auto":
		return ApprovalModeFullAuto
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func ValidateApprovalMode(mode string) error {
	switch NormalizeApprovalMode(mode) {
	case ApprovalModeReadOnly, ApprovalModeConfirm, ApprovalModeFullAuto:
		return nil
	default:
		return fmt.Errorf("unknown approval mode %q (supported: read-only, confirm, full-auto)", strings.TrimSpace(mode))
	}
}

func ApprovalModePolicyRules(mode string) ([]builtins.PolicyRule, error) {
	return ApprovalModePolicyRulesForTrust("trusted", mode)
}

func ApprovalModePolicyRulesForTrust(trust, mode string) ([]builtins.PolicyRule, error) {
	mode = NormalizeApprovalMode(mode)
	if err := ValidateApprovalMode(mode); err != nil {
		return nil, err
	}
	policy := runtime.ResolveExecutionPolicyForWorkspace("", trust, mode)
	rules := approvalModePolicyRulesForPolicy(mode, policy)
	return rules, nil
}

func approvalModePolicyRulesForPolicy(mode string, policy runtime.ExecutionPolicy) []builtins.PolicyRule {
	rules := append([]builtins.PolicyRule{}, runtime.ExecutionPolicyRules(policy)...)
	switch mode {
	case ApprovalModeReadOnly:
		denied := filterToolNames(readOnlyDeniedTools, "run_command", "http_request")
		rules = append(rules,
			builtins.DenyTool(denied...),
			builtins.DefaultAllow(),
		)
	case ApprovalModeConfirm:
		requireApproval := filterToolNames(confirmModeTools, "run_command", "http_request")
		rules = append(rules,
			builtins.RequireApprovalForPathPrefix(".git", ".moss"),
			builtins.RequireApprovalFor(requireApproval...),
			builtins.DefaultAllow(),
		)
	case ApprovalModeFullAuto:
		rules = append(rules, builtins.DefaultAllow())
	}
	return rules
}

func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	return ApplyApprovalModeWithTrust(k, "trusted", mode)
}

func ApplyApprovalModeWithTrust(k *kernel.Kernel, trust, mode string) (string, error) {
	mode = NormalizeApprovalMode(mode)
	if err := ValidateApprovalMode(mode); err != nil {
		return "", err
	}
	policy := runtime.ResolveExecutionPolicyForKernel(k, trust, mode)
	runtime.SetExecutionPolicy(k, policy)
	rules := approvalModePolicyRulesForPolicy(mode, policy)
	if len(rules) == 0 {
		return mode, nil
	}
	k.WithPolicy(rules...)
	return mode, nil
}

func filterToolNames(items []string, excluded ...string) []string {
	if len(items) == 0 {
		return nil
	}
	skip := make(map[string]struct{}, len(excluded))
	for _, item := range excluded {
		skip[item] = struct{}{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := skip[item]; ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

func EvaluatePolicy(rules []builtins.PolicyRule, spec tool.ToolSpec, input json.RawMessage) builtins.PolicyDecision {
	decision := builtins.Allow
	for _, rule := range rules {
		next := rule(builtins.PolicyContext{
			Tool:  spec,
			Input: append([]byte(nil), input...),
		})
		if next.Decision == builtins.Deny {
			return builtins.Deny
		}
		if next.Decision == builtins.RequireApproval {
			decision = builtins.RequireApproval
		}
	}
	return decision
}
