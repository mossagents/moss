package agent

import (
	mdl "github.com/mossagents/moss/kernel/model"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	cfg := AgentConfig{
		Name:         "researcher",
		SystemPrompt: "Research.",
		Tools:        []string{"grep"},
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
	if err := r.Register(AgentConfig{Name: "a", SystemPrompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(AgentConfig{Name: "b", SystemPrompt: "y"}); err != nil {
		t.Fatal(err)
	}

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
	task.Status = TaskFailed // Ensure Add stores a defensive copy.

	got, ok := tt.Get("t1")
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskRunning {
		t.Errorf("status = %q", got.Status)
	}

	got.Status = TaskCancelled // Ensure Get returns a defensive copy.
	gotAgain, _ := tt.Get("t1")
	if gotAgain.Status != TaskRunning {
		t.Fatalf("internal task should not be mutated through Get result, got %q", gotAgain.Status)
	}

	tt.Complete("t1", "done", mdl.TokenUsage{TotalTokens: 100})
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

	tt.Add(&Task{ID: "t3", AgentName: "coder", Goal: "x", Status: TaskRunning})
	tt.Add(&Task{ID: "t4", AgentName: "reviewer", Goal: "y", Status: TaskCompleted})
	running := tt.List(TaskFilter{Status: TaskRunning})
	if len(running) == 0 {
		t.Fatal("expected running tasks from List")
	}
	for _, task := range running {
		if task.Status != TaskRunning {
			t.Fatalf("unexpected task status from List: %+v", task)
		}
	}
}
