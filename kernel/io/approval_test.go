package io

import (
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

