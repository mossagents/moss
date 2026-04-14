package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

func TestBuildTurnPlan_IncludesPromptVersionFromSessionMetadata(t *testing.T) {
	sess := &session.Session{
		ID: "s1",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataInstructionProfile: "planning",
				session.MetadataPromptVersion:      "unified:abc123",
			},
		},
	}

	plan := buildTurnPlan(sess, "run-1", 1, nil)
	if plan.PromptVersion != "unified:abc123" {
		t.Fatalf("prompt version = %q", plan.PromptVersion)
	}
	if plan.InstructionProfile != "planning" {
		t.Fatalf("instruction profile = %q", plan.InstructionProfile)
	}
}

func TestBuildToolRoute_UsesToolPolicySummary(t *testing.T) {
	reg := tool.NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:         "write_file",
		Risk:         tool.RiskHigh,
		Capabilities: []string{"filesystem"},
	}, handler)); err != nil {
		t.Fatalf("Register write_file: %v", err)
	}
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:         "read_file",
		Risk:         tool.RiskLow,
		Capabilities: []string{"filesystem"},
	}, handler)); err != nil {
		t.Fatalf("Register read_file: %v", err)
	}

	sess := &session.Session{
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataToolPolicySummary: session.EncodeToolPolicySummary(session.ToolPolicySummary{
					Version:              session.ToolPolicyMetadataVersion,
					CommandAccess:        "deny",
					HTTPAccess:           "deny",
					WorkspaceWriteAccess: "deny",
					MemoryWriteAccess:    "deny",
					GraphMutationAccess:  "deny",
					DeniedClasses:        []string{string(tool.ApprovalClassSupervisorOnly)},
				}),
			},
		},
	}

	route := buildToolRoute(sess, reg, TurnPlan{})
	if len(route) != 2 {
		t.Fatalf("route len = %d, want 2", len(route))
	}

	readDecision := route[0]
	writeDecision := route[1]
	if readDecision.Name != "read_file" || writeDecision.Name != "write_file" {
		t.Fatalf("unexpected route order: %q, %q", readDecision.Name, writeDecision.Name)
	}
	if readDecision.Status != ToolRouteVisible {
		t.Fatalf("read_file status = %q", readDecision.Status)
	}
	if len(readDecision.Effects) != 1 || readDecision.Effects[0] != tool.EffectReadOnly {
		t.Fatalf("read_file effects = %v", readDecision.Effects)
	}
	if writeDecision.Status != ToolRouteHidden {
		t.Fatalf("write_file status = %q", writeDecision.Status)
	}
	if len(writeDecision.Effects) != 1 || writeDecision.Effects[0] != tool.EffectWritesWorkspace {
		t.Fatalf("write_file effects = %v", writeDecision.Effects)
	}
	if writeDecision.SideEffectClass != tool.SideEffectWorkspace {
		t.Fatalf("write_file side effect class = %q", writeDecision.SideEffectClass)
	}
	if writeDecision.ApprovalClass != tool.ApprovalClassExplicitUser {
		t.Fatalf("write_file approval class = %q", writeDecision.ApprovalClass)
	}
	if !hasReasonCode(writeDecision.ReasonCodes, "tool.effect_denied") {
		t.Fatalf("write_file reason codes = %v", writeDecision.ReasonCodes)
	}
}

func TestBuildToolRoute_HidesPlannerHiddenTools(t *testing.T) {
	reg := tool.NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:              "internal_checkpoint",
		Risk:              tool.RiskLow,
		PlannerVisibility: tool.PlannerVisibilityHidden,
	}, handler)); err != nil {
		t.Fatalf("Register internal_checkpoint: %v", err)
	}

	route := buildToolRoute(&session.Session{}, reg, TurnPlan{})
	if len(route) != 1 {
		t.Fatalf("route len = %d, want 1", len(route))
	}
	if route[0].Status != ToolRouteHidden {
		t.Fatalf("status = %q", route[0].Status)
	}
	if len(route[0].ReasonCodes) == 0 || route[0].ReasonCodes[0] != "planner_hidden" {
		t.Fatalf("reason codes = %v", route[0].ReasonCodes)
	}
}

func TestBuildToolRoute_RequiresApprovalFromSummary(t *testing.T) {
	reg := tool.NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:         "run_command",
		Risk:         tool.RiskHigh,
		Capabilities: []string{"execution"},
	}, handler)); err != nil {
		t.Fatalf("Register run_command: %v", err)
	}

	sess := &session.Session{
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataToolPolicySummary: session.EncodeToolPolicySummary(session.ToolPolicySummary{
					Version:                 session.ToolPolicyMetadataVersion,
					CommandAccess:           "require-approval",
					HTTPAccess:              "allow",
					WorkspaceWriteAccess:    "allow",
					MemoryWriteAccess:       "allow",
					GraphMutationAccess:     "allow",
					ApprovalRequiredClasses: []string{string(tool.ApprovalClassExplicitUser)},
					DeniedClasses:           []string{string(tool.ApprovalClassSupervisorOnly)},
				}),
			},
		},
	}

	route := buildToolRoute(sess, reg, TurnPlan{})
	if len(route) != 1 {
		t.Fatalf("route len = %d, want 1", len(route))
	}
	if route[0].Status != ToolRouteApprovalRequired {
		t.Fatalf("status = %q", route[0].Status)
	}
	if !hasReasonCode(route[0].ReasonCodes, "command.default_requires_approval") {
		t.Fatalf("reason codes = %v", route[0].ReasonCodes)
	}
	if !hasReasonCode(route[0].ReasonCodes, "tool.approval_class_requires_approval") {
		t.Fatalf("reason codes = %v", route[0].ReasonCodes)
	}
}

func TestBuildToolRoute_MissingSummaryUsesSafeDefaults(t *testing.T) {
	reg := tool.NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:         "write_file",
		Risk:         tool.RiskHigh,
		Capabilities: []string{"filesystem"},
	}, handler)); err != nil {
		t.Fatalf("Register write_file: %v", err)
	}

	route := buildToolRoute(&session.Session{}, reg, TurnPlan{})
	if len(route) != 1 {
		t.Fatalf("route len = %d, want 1", len(route))
	}
	if route[0].Status != ToolRouteApprovalRequired {
		t.Fatalf("status = %q", route[0].Status)
	}
	if !hasReasonCode(route[0].ReasonCodes, "policy_summary_missing") {
		t.Fatalf("reason codes = %v", route[0].ReasonCodes)
	}
	if !hasReasonCode(route[0].ReasonCodes, "safe_default_requires_approval") {
		t.Fatalf("reason codes = %v", route[0].ReasonCodes)
	}
}

func hasReasonCode(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
