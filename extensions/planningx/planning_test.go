package planningx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

func TestRegisterTools_WriteTodos(t *testing.T) {
	reg := tool.NewRegistry()
	manager := session.NewManager()
	sess, err := manager.Create(context.Background(), session.SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := RegisterTools(reg, manager); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	_, handler, ok := reg.Get("write_todos")
	if !ok {
		t.Fatal("write_todos not found")
	}

	ctx := port.WithToolCallContext(context.Background(), port.ToolCallContext{SessionID: sess.ID, ToolName: "write_todos", CallID: "c1"})
	input, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{
			{"id": "a", "title": "first", "status": "in_progress"},
			{"title": "second"},
		},
	})
	raw, err := handler(ctx, input)
	if err != nil {
		t.Fatalf("write_todos: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("unexpected status: %+v", out)
	}
	storedRaw, ok := sess.GetState(todosStateKey)
	if !ok {
		t.Fatal("todos state missing")
	}
	blob, _ := json.Marshal(storedRaw)
	var todos []TodoItem
	if err := json.Unmarshal(blob, &todos); err != nil {
		t.Fatalf("decode todos state: %v", err)
	}
	if len(todos) != 2 {
		t.Fatalf("todo count=%d", len(todos))
	}
	if todos[1].Status != "pending" {
		t.Fatalf("default status=%q", todos[1].Status)
	}
}

func TestRegisterTools_RequiresSessionContext(t *testing.T) {
	reg := tool.NewRegistry()
	manager := session.NewManager()
	if err := RegisterTools(reg, manager); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	_, handler, _ := reg.Get("write_todos")
	_, err := handler(context.Background(), json.RawMessage(`{"todos":[{"title":"x"}]}`))
	if err == nil || !strings.Contains(err.Error(), "session context") {
		t.Fatalf("expected session context error, got %v", err)
	}
}

func TestWithSessionManager_BootAndPrompt(t *testing.T) {
	manager := session.NewManager()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&port.NoOpIO{}),
		WithSessionManager(manager),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if _, _, ok := k.ToolRegistry().Get("write_todos"); !ok {
		t.Fatal("write_todos should be registered after boot")
	}
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt")
	}
	if !strings.Contains(sess.Messages[0].Content, "write_todos") {
		t.Fatalf("missing prompt hint: %q", sess.Messages[0].Content)
	}
}
