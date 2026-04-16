package planning_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/harness/runtime/planning"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

func TestRegisterPlanningTools_NilManager(t *testing.T) {
	reg := tool.NewRegistry()
	err := planning.RegisterPlanningTools(reg, nil)
	if err == nil {
		t.Fatal("expected error for nil session manager")
	}
}

func TestRegisterPlanningTools_Success(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()

	err := planning.RegisterPlanningTools(reg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := reg.Get("update_plan")
	if !ok {
		t.Fatal("expected update_plan to be registered")
	}
}

func TestRegisterPlanningTools_Idempotent(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()

	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatalf("expected no error on re-registration: %v", err)
	}
}

func TestWithPlanningDefaults(t *testing.T) {
	k := kernel.New()
	k.Apply(planning.WithPlanningDefaults())
}

func TestWithPlanningSessionManager(t *testing.T) {
	mgr := session.NewManager()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&kernio.NoOpIO{}),
		planning.WithPlanningSessionManager(mgr),
	)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("unexpected boot error: %v", err)
	}
	_, ok := k.ToolRegistry().Get("update_plan")
	if !ok {
		t.Fatal("expected update_plan after boot with session manager")
	}
}

func TestUpdatePlanTool_NoSessionContext(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Get("update_plan")
	if !ok {
		t.Fatal("update_plan not found")
	}
	input, _ := json.Marshal(map[string]any{
		"items": []map[string]any{{"title": "task1"}},
	})
	_, err := entry.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when no session context")
	}
}

func TestUpdatePlanTool_WithSessionContextStoresUnifiedState(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Get("update_plan")
	if !ok {
		t.Fatal("update_plan not found")
	}

	sess, err := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithToolCallContext(context.Background(), tool.ToolCallContext{
		SessionID: sess.ID,
		ToolName:  "update_plan",
	})

	input, _ := json.Marshal(map[string]any{
		"goal":          "Ship unified planning",
		"explanation":   "Keep one source of truth for plan and progress.",
		"current_focus": "implement-core",
		"items": []map[string]any{
			{
				"id":          "design-state",
				"title":       "Design planning state",
				"status":      "completed",
				"description": "Define the unified model.",
			},
			{
				"id":          "implement-core",
				"title":       "Implement update_plan",
				"status":      "in_progress",
				"depends_on":  []string{"design-state"},
				"description": "Replace the old planning tool.",
			},
		},
	})
	result, err := entry.Execute(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Status       string         `json:"status"`
		CurrentFocus string         `json:"current_focus"`
		ItemCount    int            `json:"item_count"`
		Plan         planning.State `json:"plan"`
		PlanMarkdown string         `json:"plan_markdown"`
		TodoMarkdown string         `json:"todo_markdown"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("expected status=ok, got %q", out.Status)
	}
	if out.ItemCount != 2 {
		t.Fatalf("expected item_count=2, got %d", out.ItemCount)
	}
	if out.CurrentFocus != "implement-core" {
		t.Fatalf("unexpected current_focus %q", out.CurrentFocus)
	}
	if !strings.Contains(out.PlanMarkdown, "Ship unified planning") {
		t.Fatalf("missing goal in plan markdown: %q", out.PlanMarkdown)
	}
	if !strings.Contains(out.TodoMarkdown, "Implement update_plan") {
		t.Fatalf("missing todo item in todo markdown: %q", out.TodoMarkdown)
	}
	state, ok := planning.ReadSessionPlan(sess)
	if !ok {
		t.Fatal("expected session planning state")
	}
	if state.CurrentFocus != "implement-core" {
		t.Fatalf("unexpected stored current focus %q", state.CurrentFocus)
	}
	if len(state.Items) != 2 || state.Items[1].DependsOn[0] != "design-state" {
		t.Fatalf("unexpected stored items: %+v", state.Items)
	}
}

func TestUpdatePlanTool_RejectsEmptyItems(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, _ := reg.Get("update_plan")
	sess, _ := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	ctx := tool.WithToolCallContext(context.Background(), tool.ToolCallContext{
		SessionID: sess.ID,
	})
	input, _ := json.Marshal(map[string]any{"items": []any{}})
	_, err := entry.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected error for empty items")
	}
}

func TestUpdatePlanTool_RejectsMultipleInProgressItems(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, _ := reg.Get("update_plan")
	sess, _ := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	ctx := tool.WithToolCallContext(context.Background(), tool.ToolCallContext{
		SessionID: sess.ID,
	})
	input, _ := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"id": "a", "title": "task a", "status": "in_progress"},
			{"id": "b", "title": "task b", "status": "in_progress"},
		},
	})
	_, err := entry.Execute(ctx, input)
	if err == nil || !strings.Contains(err.Error(), "at most one item can be in_progress") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatePlanTool_RejectsUnknownDependencies(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := planning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, _ := reg.Get("update_plan")
	sess, _ := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	ctx := tool.WithToolCallContext(context.Background(), tool.ToolCallContext{
		SessionID: sess.ID,
	})
	input, _ := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"id": "task-a", "title": "task a", "depends_on": []string{"task-b"}},
		},
	})
	_, err := entry.Execute(ctx, input)
	if err == nil || !strings.Contains(err.Error(), "depends on unknown item") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeState_AssignsFocusFromInProgressItem(t *testing.T) {
	state, err := planning.NormalizeState(planning.UpdateInput{
		Goal: "ship it",
		Items: []planning.Item{
			{Title: "design", Status: "completed"},
			{Title: "implement", Status: "in_progress"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeState: %v", err)
	}
	if state.CurrentFocus != "implement" {
		t.Fatalf("unexpected current focus %q", state.CurrentFocus)
	}
}
