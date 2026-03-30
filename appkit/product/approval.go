package product

import (
	"encoding/json"
	"fmt"
	"strings"

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
	mode = NormalizeApprovalMode(mode)
	if err := ValidateApprovalMode(mode); err != nil {
		return nil, err
	}
	rules := []builtins.PolicyRule{
		builtins.DenyCommandContaining("rm -rf /", "format c:", "del /f /q c:\\"),
	}
	switch mode {
	case ApprovalModeReadOnly:
		rules = append(rules,
			builtins.DenyTool(readOnlyDeniedTools...),
			builtins.DefaultAllow(),
		)
	case ApprovalModeConfirm:
		rules = append(rules,
			builtins.RequireApprovalForPathPrefix(".git", ".moss"),
			builtins.RequireApprovalFor(confirmModeTools...),
			builtins.DefaultAllow(),
		)
	case ApprovalModeFullAuto:
		rules = append(rules, builtins.DefaultAllow())
	}
	return rules, nil
}

func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	rules, err := ApprovalModePolicyRules(mode)
	if err != nil {
		return "", err
	}
	k.WithPolicy(rules...)
	return NormalizeApprovalMode(mode), nil
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
