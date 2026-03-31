package builtins

import (
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestCommandRulesRequireApprovalForMatchedCommand(t *testing.T) {
	rule := CommandRules(CommandPatternRule{
		Name:   "git-push",
		Match:  "git push*",
		Access: RequireApproval,
	})

	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	result := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "run_command"},
		Input: input,
	})
	if result.Decision != RequireApproval {
		t.Fatalf("decision = %s, want %s", result.Decision, RequireApproval)
	}
	if result.Reason.Code != "command.rule_requires_approval" {
		t.Fatalf("reason code = %q", result.Reason.Code)
	}
}

func TestCommandRulesDenyWinsOverApproval(t *testing.T) {
	rule := CommandRules(
		CommandPatternRule{Match: "git *", Access: RequireApproval},
		CommandPatternRule{Name: "git-push", Match: "git push*", Access: Deny},
	)

	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	result := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "run_command"},
		Input: input,
	})
	if result.Decision != Deny {
		t.Fatalf("decision = %s, want %s", result.Decision, Deny)
	}
	if result.Reason.Code != "command.rule_denied" {
		t.Fatalf("reason code = %q", result.Reason.Code)
	}
}

func TestCommandRulesIgnoreOtherTools(t *testing.T) {
	rule := CommandRules(CommandPatternRule{Match: "git *", Access: Deny})
	result := rule(PolicyContext{Tool: tool.ToolSpec{Name: "write_file"}})
	if result.Decision != Allow {
		t.Fatalf("decision = %s, want %s", result.Decision, Allow)
	}
}
