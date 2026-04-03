package port

import (
	"context"
	"errors"
	"path/filepath"
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

func TestFileTaskRuntime_PersistsStateAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tasks")
	ctx := context.Background()

	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "dep", Status: TaskCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "a", AgentName: "worker", Status: TaskPending, DependsOn: []string{"dep"}}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	task, err := reloaded.GetTask(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskPending || len(task.DependsOn) != 1 || task.DependsOn[0] != "dep" {
		t.Fatalf("unexpected persisted task: %+v", task)
	}

	claimed, err := reloaded.ClaimNextReady(ctx, "agent-1", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != TaskRunning || claimed.ClaimedBy != "agent-1" {
		t.Fatalf("unexpected claimed task: %+v", claimed)
	}

	restarted, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := restarted.GetTask(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != TaskRunning || persisted.ClaimedBy != "agent-1" {
		t.Fatalf("expected running persisted claim, got %+v", persisted)
	}
}

func TestMemoryTaskRuntime_JobRuntimeAndTransitions(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job1", AgentName: "worker", Goal: "g1", Status: JobPending}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job1", AgentName: "worker", Goal: "g1", Status: JobRunning}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJobItem(ctx, AgentJobItem{JobID: "job1", ItemID: "item1", Status: JobPending}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJobItem(ctx, AgentJobItem{JobID: "job1", ItemID: "item1", Status: JobRunning}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJobItem(ctx, AgentJobItem{JobID: "job1", ItemID: "item1", Status: JobCompleted}); err != nil {
		t.Fatal(err)
	}
	items, err := rt.ListJobItems(ctx, JobItemQuery{JobID: "job1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != JobCompleted {
		t.Fatalf("unexpected items: %+v", items)
	}
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job1", AgentName: "worker", Goal: "g1", Status: JobCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job1", AgentName: "worker", Goal: "g1", Status: JobRunning}); !errors.Is(err, ErrInvalidJobTransition) {
		t.Fatalf("expected ErrInvalidJobTransition, got %v", err)
	}
}

func TestFileTaskRuntime_PersistsJobsAndItemsAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jobs")
	ctx := context.Background()

	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJob(ctx, AgentJob{ID: "jobA", AgentName: "worker", Goal: "goal", Status: JobPending}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJobItem(ctx, AgentJobItem{JobID: "jobA", ItemID: "i1", Status: JobRunning, Executor: "agent-1"}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	job, err := reloaded.GetJob(ctx, "jobA")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPending {
		t.Fatalf("unexpected job: %+v", job)
	}
	items, err := reloaded.ListJobItems(ctx, JobItemQuery{JobID: "jobA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Executor != "agent-1" || items[0].Status != JobRunning {
		t.Fatalf("unexpected items after reload: %+v", items)
	}
}
