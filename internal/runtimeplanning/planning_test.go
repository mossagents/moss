package runtimeplanning_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/internal/runtimeplanning"
	kernio "github.com/mossagents/moss/kernel/io"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

func TestRegisterPlanningTools_NilManager(t *testing.T) {
	reg := tool.NewRegistry()
	err := runtimeplanning.RegisterPlanningTools(reg, nil)
	if err == nil {
		t.Fatal("expected error for nil session manager")
	}
}

func TestRegisterPlanningTools_Success(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()

	err := runtimeplanning.RegisterPlanningTools(reg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := reg.Get("write_todos")
	if !ok {
		t.Fatal("expected write_todos to be registered")
	}
}

func TestRegisterPlanningTools_Idempotent(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()

	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	// Second call should be a no-op (already registered)
	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatalf("expected no error on re-registration: %v", err)
	}
}

func TestWithPlanningDefaults(t *testing.T) {
	// WithPlanningDefaults is WithPlanningSessionManager(nil); should not panic on apply
	k := kernel.New()
	k.Apply(runtimeplanning.WithPlanningDefaults())
}

func TestWithPlanningSessionManager(t *testing.T) {
	mgr := session.NewManager()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&kernio.NoOpIO{}),
		runtimeplanning.WithPlanningSessionManager(mgr),
	)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("unexpected boot error: %v", err)
	}
	_, ok := k.ToolRegistry().Get("write_todos")
	if !ok {
		t.Fatal("expected write_todos after boot with session manager")
	}
}

func TestWriteTodosTool_NoSessionContext(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Get("write_todos")
	if !ok {
		t.Fatal("write_todos not found")
	}
	// Call without tool call context in context
	input, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{{"title": "task1"}},
	})
	_, err := entry.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when no session context")
	}
}

func TestWriteTodosTool_WithSessionContext(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Get("write_todos")
	if !ok {
		t.Fatal("write_todos not found")
	}

	// Create a real session
	sess, err := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatal(err)
	}

	// Inject session context
	ctx := toolctx.WithToolCallContext(context.Background(), toolctx.ToolCallContext{
		SessionID: sess.ID,
		ToolName:  "write_todos",
	})

	input, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{
			{"title": "implement feature", "status": "pending"},
		},
	})
	result, err := entry.Execute(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", out["status"])
	}
	count, _ := out["count"].(float64)
	if count != 1 {
		t.Errorf("expected count=1, got %v", out["count"])
	}
}

func TestWriteTodosTool_EmptyTodos(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, _ := reg.Get("write_todos")

	sess, _ := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	ctx := toolctx.WithToolCallContext(context.Background(), toolctx.ToolCallContext{
		SessionID: sess.ID,
	})

	input, _ := json.Marshal(map[string]any{"todos": []any{}})
	_, err := entry.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected error for empty todos")
	}
}

func TestWriteTodosTool_MergeMode(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := session.NewManager()
	if err := runtimeplanning.RegisterPlanningTools(reg, mgr); err != nil {
		t.Fatal(err)
	}
	entry, _ := reg.Get("write_todos")

	sess, _ := mgr.Create(context.Background(), session.SessionConfig{Goal: "test"})
	ctx := toolctx.WithToolCallContext(context.Background(), toolctx.ToolCallContext{
		SessionID: sess.ID,
	})

	// First write
	input1, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{
			{"id": "t1", "title": "task 1"},
		},
	})
	_, err := entry.Execute(ctx, input1)
	if err != nil {
		t.Fatal(err)
	}

	// Second write in merge mode (replace=false)
	replace := false
	input2, _ := json.Marshal(map[string]any{
		"todos":   []map[string]any{{"id": "t2", "title": "task 2"}},
		"replace": replace,
	})
	result, err := entry.Execute(ctx, input2)
	if err != nil {
		t.Fatalf("unexpected error in merge mode: %v", err)
	}
	var out map[string]any
	json.Unmarshal(result, &out)
	count, _ := out["count"].(float64)
	if count != 2 {
		t.Errorf("expected 2 todos after merge, got %v", out["count"])
	}
}
