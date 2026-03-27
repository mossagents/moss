package port

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryTaskRuntime_ClaimReady(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()

	if err := rt.UpsertTask(ctx, TaskRecord{ID: "dep", Status: TaskCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "a", AgentName: "worker", Status: TaskPending, DependsOn: []string{"dep"}}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "b", AgentName: "worker", Status: TaskPending, DependsOn: []string{"missing"}}); err != nil {
		t.Fatal(err)
	}

	got, err := rt.ClaimNextReady(ctx, "agent-1", "worker")
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if got.ID != "a" || got.Status != TaskRunning || got.ClaimedBy != "agent-1" {
		t.Fatalf("unexpected claimed task: %+v", got)
	}

	_, err = rt.ClaimNextReady(ctx, "agent-2", "worker")
	if !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("expected ErrNoReadyTask, got %v", err)
	}
}

