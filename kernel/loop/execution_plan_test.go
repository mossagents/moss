package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
)

func TestBuildExecutionPlanPopulatesToolSemantics(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{
		Name:              "write_file",
		Effects:           []tool.Effect{tool.EffectWritesWorkspace},
		ResourceScope:     []string{"workspace:*"},
		LockScope:         []string{"workspace:*"},
		SideEffectClass:   tool.SideEffectWorkspace,
		ApprovalClass:     tool.ApprovalClassExplicitUser,
		PlannerVisibility: tool.PlannerVisibilityVisibleWithConstraints,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	plan, err := buildExecutionPlan([]model.ToolCall{{
		ID:        "call-1",
		Name:      "write_file",
		Arguments: json.RawMessage(`{"path":"x","content":"y"}`),
	}}, reg)
	if err != nil {
		t.Fatalf("buildExecutionPlan: %v", err)
	}
	if len(plan.Calls) != 1 {
		t.Fatalf("call len = %d, want 1", len(plan.Calls))
	}
	call := plan.Calls[0]
	if call.CallID != "call-1" || call.ToolName != "write_file" {
		t.Fatalf("unexpected plan call: %+v", call)
	}
	if len(call.DeclaredEffects) != 1 || call.DeclaredEffects[0] != tool.EffectWritesWorkspace {
		t.Fatalf("declared_effects = %v", call.DeclaredEffects)
	}
	if len(call.ResourceScope) != 1 || call.ResourceScope[0] != "workspace:*" {
		t.Fatalf("resource_scope = %v", call.ResourceScope)
	}
	if len(call.LockScope) != 1 || call.LockScope[0] != "workspace:*" {
		t.Fatalf("lock_scope = %v", call.LockScope)
	}
}

func TestExecutionPlanValidateRejectsInvalidDependencies(t *testing.T) {
	err := (ExecutionPlan{
		Calls: []ExecutionPlanCall{
			{CallID: "a", ToolName: "read_file", DeclaredEffects: []tool.Effect{tool.EffectReadOnly}, ResourceScope: []string{}, LockScope: []string{}, DependsOn: []string{"b"}},
			{CallID: "b", ToolName: "read_file", DeclaredEffects: []tool.Effect{tool.EffectReadOnly}, ResourceScope: []string{}, LockScope: []string{}, DependsOn: []string{"a"}},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected cycle validation error")
	}
}

func TestExecutionPlanValidateRejectsUnknownScopeRoot(t *testing.T) {
	err := (ExecutionPlan{
		Calls: []ExecutionPlanCall{{
			CallID:          "a",
			ToolName:        "read_file",
			DeclaredEffects: []tool.Effect{tool.EffectReadOnly},
			ResourceScope:   []string{"invalid:*"},
			LockScope:       []string{},
			DependsOn:       []string{},
		}},
	}).Validate()
	if err == nil {
		t.Fatal("expected unknown scope root error")
	}
}
