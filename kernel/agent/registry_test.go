package agent

import (
	"testing"

	"github.com/mossagi/moss/kernel/port"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	cfg := AgentConfig{
		Name:         "researcher",
		SystemPrompt: "Research.",
		Tools:        []string{"search_text"},
	}
	if err := r.Register(cfg); err != nil {
		t.Fatal(err)
	}

	got, ok := r.Get("researcher")
	if !ok {
		t.Fatal("researcher not found")
	}
	if got.Name != "researcher" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := NewRegistry()

	cfg := AgentConfig{Name: "a", SystemPrompt: "x"}
	if err := r.Register(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(cfg); err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.Register(AgentConfig{Name: "a", SystemPrompt: "x"})
	r.Register(AgentConfig{Name: "b", SystemPrompt: "y"})

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("got %d agents, want 2", len(list))
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestDepth(t *testing.T) {
	ctx := WithDepth(t.Context(), 0)
	if d := Depth(ctx); d != 0 {
		t.Errorf("depth = %d, want 0", d)
	}

	ctx = WithDepth(ctx, 2)
	if d := Depth(ctx); d != 2 {
		t.Errorf("depth = %d, want 2", d)
	}
}

func TestTaskTracker(t *testing.T) {
	tt := NewTaskTracker()

	task := &Task{ID: "t1", AgentName: "a", Goal: "test", Status: TaskRunning}
	tt.Add(task)

	got, ok := tt.Get("t1")
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskRunning {
		t.Errorf("status = %q", got.Status)
	}

	tt.Complete("t1", "done", port.TokenUsage{TotalTokens: 100})
	got, _ = tt.Get("t1")
	if got.Status != TaskCompleted {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if got.Result != "done" {
		t.Errorf("result = %q", got.Result)
	}

	tt.Add(&Task{ID: "t2", Status: TaskRunning})
	tt.Fail("t2", "oops")
	got, _ = tt.Get("t2")
	if got.Status != TaskFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
}
