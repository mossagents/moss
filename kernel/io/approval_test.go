package io

import (
	"strings"
	"testing"
)

func TestNormalizeApprovalRequestInfersScopesAndBinding(t *testing.T) {
	req := NormalizeApprovalRequest(&ApprovalRequest{
		ToolName:            "run_command",
		Category:            ApprovalCategoryCommand,
		ScopeLabel:          "Matching rule",
		ScopeValue:          "git push",
		CacheKey:            "command:git push",
		SessionDecisionNote: "remember for session",
		ProjectDecisionNote: "remember for project",
	})
	if len(req.AllowedScopes) != 3 {
		t.Fatalf("expected once/session/project scopes, got %+v", req.AllowedScopes)
	}
	if req.DefaultScope != DecisionScopeOnce || req.DefaultPersistence != DecisionPersistenceRequest {
		t.Fatalf("unexpected defaults: scope=%s persistence=%s", req.DefaultScope, req.DefaultPersistence)
	}
	if req.RuleBinding == nil || req.RuleBinding.CacheKey != "command:git push" {
		t.Fatalf("expected inferred rule binding, got %+v", req.RuleBinding)
	}
}

func TestNormalizeApprovalDecisionForRequestAssignsScopePersistence(t *testing.T) {
	req := NormalizeApprovalRequest(&ApprovalRequest{
		CacheKey:            "rule-1",
		SessionDecisionNote: "session",
		ProjectDecisionNote: "project",
	})
	decision := NormalizeApprovalDecisionForRequest(req, &ApprovalDecision{
		RequestID: "approval-1",
		Type:      ApprovalDecisionPolicyAmendment,
	})
	if !decision.Approved {
		t.Fatal("policy amendment should normalize to approved")
	}
	if decision.Scope != DecisionScopeProject || decision.Persistence != DecisionPersistenceProject {
		t.Fatalf("unexpected decision scope/persistence: %+v", decision)
	}
	if decision.CacheKey != "rule-1" {
		t.Fatalf("expected cache key propagation, got %+v", decision)
	}
}

func TestFormatApprovalPrompt(t *testing.T) {
	t.Run("nil request uses default", func(t *testing.T) {
		got := FormatApprovalPrompt(nil)
		if got != "Allow requested action?" {
			t.Fatalf("unexpected: %s", got)
		}
	})

	t.Run("empty prompt falls back to default", func(t *testing.T) {
		got := FormatApprovalPrompt(&ApprovalRequest{})
		if got != "Allow requested action?" {
			t.Fatalf("unexpected: %s", got)
		}
	})

	t.Run("prompt only, no details", func(t *testing.T) {
		got := FormatApprovalPrompt(&ApprovalRequest{Prompt: "Run git push?"})
		if got != "Run git push?" {
			t.Fatalf("unexpected: %s", got)
		}
	})

	t.Run("full details appended", func(t *testing.T) {
		got := FormatApprovalPrompt(&ApprovalRequest{
			Prompt:      "Run command?",
			ToolName:    "bash",
			Risk:        "high",
			Reason:      "deploy",
			ReasonCode:  "code-42",
			Enforcement: "strict",
		})
		if !strings.HasPrefix(got, "Run command? (") {
			t.Fatalf("unexpected format: %s", got)
		}
		if !strings.Contains(got, "tool=bash") || !strings.Contains(got, "risk=high") {
			t.Fatalf("missing details: %s", got)
		}
	})
}

func TestNormalizeApprovalDecision(t *testing.T) {
	d := NormalizeApprovalDecision(&ApprovalDecision{
		RequestID: "r1",
		Type:      ApprovalDecisionApproveSession,
	})
	if d.Scope != DecisionScopeSession {
		t.Fatalf("expected session scope, got %s", d.Scope)
	}
	if !d.Approved {
		t.Fatal("ApproveSession should normalize to approved=true")
	}
}

func TestFormatDeniedMessage(t *testing.T) {
	t.Run("no tool name", func(t *testing.T) {
		got := FormatDeniedMessage("", "", "", "")
		if got != "Tool call denied by policy." {
			t.Fatalf("unexpected: %s", got)
		}
	})

	t.Run("with tool name only", func(t *testing.T) {
		got := FormatDeniedMessage("bash", "", "", "")
		if got != "Tool bash denied by policy." {
			t.Fatalf("unexpected: %s", got)
		}
	})

	t.Run("all fields", func(t *testing.T) {
		got := FormatDeniedMessage("bash", "not allowed", "POLICY_001", "strict")
		if !strings.Contains(got, "Tool bash denied") {
			t.Fatalf("missing tool: %s", got)
		}
		if !strings.Contains(got, "reason=not allowed") {
			t.Fatalf("missing reason: %s", got)
		}
		if !strings.Contains(got, "reason_code=POLICY_001") {
			t.Fatalf("missing reason_code: %s", got)
		}
		if !strings.Contains(got, "enforcement=strict") {
			t.Fatalf("missing enforcement: %s", got)
		}
	})
}

