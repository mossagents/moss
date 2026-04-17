package guardian

import (
	"context"
	"testing"

	kt "github.com/mossagents/moss/kernel/testing"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
)

func TestReviewToolApproval(t *testing.T) {
	g := New(&kt.MockLLM{Responses: []model.CompletionResponse{{
		Message: model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart(`{"approved":true,"reason":"safe narrow action","confidence":"high"}`)}},
		StopReason: "end_turn",
	}}}, model.ModelConfig{Model: "guardian", Temperature: 0})
	result, err := g.ReviewToolApproval(context.Background(), ReviewInput{ToolName: "read_file", Reason: "inspect config"})
	if err != nil {
		t.Fatalf("ReviewToolApproval: %v", err)
	}
	if result == nil || !result.Approved || result.Confidence != "high" {
		t.Fatalf("unexpected review result: %+v", result)
	}
}

func TestAutoApprovalDecisionRequiresHighConfidence(t *testing.T) {
	req := &io.ApprovalRequest{ID: "approval-1"}
	if decision := AutoApprovalDecision(req, &ReviewResult{Approved: true, Confidence: "medium"}); decision != nil {
		t.Fatalf("expected nil decision for medium confidence, got %+v", decision)
	}
	decision := AutoApprovalDecision(req, &ReviewResult{Approved: true, Confidence: "high", Reason: "safe"})
	if decision == nil || !decision.Approved || decision.Source != "guardian" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}