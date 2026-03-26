package agent

import (
	"context"
	"testing"
)

func TestInMemoryTaskStore_SaveLoad(t *testing.T) {
	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:        "t-1",
		AgentName: "coder",
		Goal:      "write tests",
		Status:    TaskRunning,
	}

	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(ctx, "t-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.ID != "t-1" || loaded.Goal != "write tests" {
		t.Fatalf("unexpected loaded task: %+v", loaded)
	}

	// 修改原始对象不应影响存储
	task.Goal = "modified"
	loaded2, _ := store.Load(ctx, "t-1")
	if loaded2.Goal != "write tests" {
		t.Fatal("store should return copies")
	}
}

func TestInMemoryTaskStore_LoadNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task, err := store.Load(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if task != nil {
		t.Fatal("expected nil for nonexistent task")
	}
}

func TestInMemoryTaskStore_List(t *testing.T) {
	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t-1", AgentName: "coder", Status: TaskRunning},
		{ID: "t-2", AgentName: "coder", Status: TaskCompleted},
		{ID: "t-3", AgentName: "reviewer", Status: TaskRunning},
	}
	for _, task := range tasks {
		if err := store.Save(ctx, task); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// 无过滤
	all, err := store.List(ctx, TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// 按 agent 过滤
	coderTasks, err := store.List(ctx, TaskFilter{AgentName: "coder"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(coderTasks) != 2 {
		t.Fatalf("expected 2 coder tasks, got %d", len(coderTasks))
	}

	// 按状态过滤
	running, err := store.List(ctx, TaskFilter{Status: TaskRunning})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("expected 2 running tasks, got %d", len(running))
	}

	// 组合过滤
	coderRunning, err := store.List(ctx, TaskFilter{AgentName: "coder", Status: TaskRunning})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(coderRunning) != 1 {
		t.Fatalf("expected 1 coder running task, got %d", len(coderRunning))
	}
}
