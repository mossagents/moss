package distributed_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mossagents/moss/distributed"
	"github.com/mossagents/moss/kernel/port"
)

func setupServer() (*httptest.Server, *distributed.RemoteTaskRuntime) {
	rt := port.NewMemoryTaskRuntime()
	srv := distributed.NewTaskRuntimeServer(rt, rt, rt)
	ts := httptest.NewServer(srv.Handler())
	remote := distributed.NewRemoteTaskRuntime(ts.URL)
	return ts, remote
}

func TestRemoteTaskRuntime_UpsertAndGet(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	task := port.TaskRecord{
		ID:        "t1",
		AgentName: "agent-a",
		Goal:      "do something",
		Status:    port.TaskPending,
	}
	if err := remote.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	got, err := remote.GetTask(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Goal != task.Goal {
		t.Errorf("got goal %q, want %q", got.Goal, task.Goal)
	}
}

func TestRemoteTaskRuntime_GetNotFound(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	_, err := remote.GetTask(context.Background(), "missing")
	if err != port.ErrTaskNotFound {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestRemoteTaskRuntime_ListAndClaim(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := remote.UpsertTask(ctx, port.TaskRecord{
			ID:        id,
			AgentName: "worker",
			Goal:      "task-" + id,
			Status:    port.TaskPending,
		}); err != nil {
			t.Fatalf("UpsertTask %s: %v", id, err)
		}
		time.Sleep(1 * time.Millisecond) // ensure ordering
	}

	tasks, err := remote.ListTasks(ctx, port.TaskQuery{AgentName: "worker"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("got %d tasks, want 3", len(tasks))
	}

	claimed, err := remote.ClaimNextReady(ctx, "exec-1", "worker")
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claimed.ID != "a" {
		t.Errorf("expected first task 'a', got %q", claimed.ID)
	}
	if claimed.Status != port.TaskRunning {
		t.Errorf("expected status running, got %q", claimed.Status)
	}
}

func TestRemoteTaskRuntime_Jobs(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	job := port.AgentJob{
		ID:        "job-1",
		AgentName: "worker",
		Goal:      "batch process",
		Status:    port.JobPending,
	}
	if err := remote.UpsertJob(ctx, job); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	got, err := remote.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Goal != job.Goal {
		t.Errorf("got goal %q, want %q", got.Goal, job.Goal)
	}

	// Add items
	for i, id := range []string{"i1", "i2"} {
		_ = i
		if err := remote.UpsertJobItem(ctx, port.AgentJobItem{
			JobID:  "job-1",
			ItemID: id,
			Status: port.JobPending,
		}); err != nil {
			t.Fatalf("UpsertJobItem %s: %v", id, err)
		}
	}
	items, err := remote.ListJobItems(ctx, port.JobItemQuery{JobID: "job-1"})
	if err != nil {
		t.Fatalf("ListJobItems: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("got %d items, want 2", len(items))
	}

	// Mark running
	item, err := remote.MarkJobItemRunning(ctx, "job-1", "i1", "worker-1")
	if err != nil {
		t.Fatalf("MarkJobItemRunning: %v", err)
	}
	if item.Status != port.JobRunning {
		t.Errorf("expected running, got %q", item.Status)
	}

	// Report result
	done, err := remote.ReportJobItemResult(ctx, "job-1", "i1", "worker-1", port.JobCompleted, "ok", "")
	if err != nil {
		t.Fatalf("ReportJobItemResult: %v", err)
	}
	if done.Status != port.JobCompleted {
		t.Errorf("expected completed, got %q", done.Status)
	}
}

func TestInProcessLock(t *testing.T) {
	lock := distributed.NewInProcessLock()
	ctx := context.Background()

	token, err := lock.Acquire(ctx, "res", 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Second acquire should fail
	_, err2 := lock.Acquire(ctx, "res", 5*time.Second)
	if err2 == nil {
		t.Fatal("expected error on double-acquire")
	}

	// Refresh should work
	if err := lock.Refresh(ctx, "res", token, 10*time.Second); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Release
	if err := lock.Release(ctx, "res", token); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Now acquire again should succeed
	tok2, err := lock.Acquire(ctx, "res", 5*time.Second)
	if err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	_ = lock.Release(ctx, "res", tok2)
}
