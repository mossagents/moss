package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
)

const serviceKey kernel.ServiceKey = "guardian.reviewer"

type ReviewInput struct {
	SessionID   string          `json:"session_id,omitempty"`
	SessionGoal string          `json:"session_goal,omitempty"`
	ToolName    string          `json:"tool_name"`
	Risk        string          `json:"risk,omitempty"`
	Category    string          `json:"category,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	ReasonCode  string          `json:"reason_code,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
}

type ReviewResult struct {
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

type Guardian struct {
	LLM         model.LLM
	ModelConfig model.ModelConfig
}

func New(llm model.LLM, cfg model.ModelConfig) *Guardian {
	return &Guardian{LLM: llm, ModelConfig: cfg}
}

func Install(k *kernel.Kernel, g *Guardian) {
	if k == nil || g == nil {
		return
	}
	k.Services().Store(serviceKey, g)
}

func Lookup(k *kernel.Kernel) (*Guardian, bool) {
	if k == nil {
		return nil, false
	}
	v, ok := k.Services().Load(serviceKey)
	if !ok || v == nil {
		return nil, false
	}
	g, ok := v.(*Guardian)
	return g, ok && g != nil
}

func (g *Guardian) ReviewToolApproval(ctx context.Context, input ReviewInput) (*ReviewResult, error) {
	if g == nil || g.LLM == nil {
		return nil, nil
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal guardian input: %w", err)
	}
	resp, err := model.Complete(ctx, g.LLM, model.CompletionRequest{
		Messages: []model.Message{
			{
				Role: model.RoleSystem,
				ContentParts: []model.ContentPart{model.TextPart("You are a conservative tool-approval guardian. Return JSON only with fields approved(boolean), reason(string), confidence(string). Approve only when the action is narrow in scope, low blast radius, and clearly justified by the request. If uncertain, set approved to false.")},
			},
			{
				Role: model.RoleUser,
				ContentParts: []model.ContentPart{model.TextPart(string(payload))},
			},
		},
		Config: g.ModelConfig,
		ResponseFormat: &model.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, err
	}
	var out ReviewResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(model.ContentPartsToPlainText(resp.Message.ContentParts))), &out); err != nil {
		return nil, fmt.Errorf("decode guardian response: %w", err)
	}
	out.Confidence = strings.ToLower(strings.TrimSpace(out.Confidence))
	out.Reason = strings.TrimSpace(out.Reason)
	return &out, nil
}

func AutoApprovalDecision(req *io.ApprovalRequest, review *ReviewResult) *io.ApprovalDecision {
	if req == nil || review == nil || !review.Approved || review.Confidence != "high" {
		return nil
	}
	return io.NormalizeApprovalDecisionForRequest(req, &io.ApprovalDecision{
		RequestID: req.ID,
		Type:      io.ApprovalDecisionApprove,
		Approved:  true,
		Reason:    strings.TrimSpace(review.Reason),
		Source:    "guardian",
	})
}