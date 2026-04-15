package distributed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mossagents/moss/harness/distributed"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func setupServer() (*httptest.Server, *distributed.RemoteTaskRuntime) {
	rt := taskrt.NewMemoryTaskRuntime()
	srv := distributed.NewTaskRuntimeServer(rt, rt, rt)
	ts := httptest.NewServer(srv.Handler())
	remote := distributed.NewRemoteTaskRuntime(ts.URL)
	return ts, remote
}

func TestRemoteTaskRuntime_UpsertAndGet(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	task := taskrt.TaskRecord{
		ID:        "t1",
		AgentName: "agent-a",
		Goal:      "do something",
		Status:    taskrt.TaskPending,
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
	if err != taskrt.ErrTaskNotFound {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestRemoteTaskRuntime_ListAndClaim(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := remote.UpsertTask(ctx, taskrt.TaskRecord{
			ID:        id,
			AgentName: "worker",
			Goal:      "task-" + id,
			Status:    taskrt.TaskPending,
		}); err != nil {
			t.Fatalf("UpsertTask %s: %v", id, err)
		}
		time.Sleep(1 * time.Millisecond) // ensure ordering
	}

	tasks, err := remote.ListTasks(ctx, taskrt.TaskQuery{AgentName: "worker"})
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
	if claimed.Status != taskrt.TaskRunning {
		t.Errorf("expected status running, got %q", claimed.Status)
	}
}

func TestRemoteTaskRuntime_Jobs(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	job := taskrt.AgentJob{
		ID:        "job-1",
		AgentName: "worker",
		Goal:      "batch process",
		Status:    taskrt.JobPending,
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
		if err := remote.UpsertJobItem(ctx, taskrt.AgentJobItem{
			JobID:  "job-1",
			ItemID: id,
			Status: taskrt.JobPending,
		}); err != nil {
			t.Fatalf("UpsertJobItem %s: %v", id, err)
		}
	}
	items, err := remote.ListJobItems(ctx, taskrt.JobItemQuery{JobID: "job-1"})
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
	if item.Status != taskrt.JobRunning {
		t.Errorf("expected running, got %q", item.Status)
	}

	// Report result
	done, err := remote.ReportJobItemResult(ctx, "job-1", "i1", "worker-1", taskrt.JobCompleted, "ok", "")
	if err != nil {
		t.Fatalf("ReportJobItemResult: %v", err)
	}
	if done.Status != taskrt.JobCompleted {
		t.Errorf("expected completed, got %q", done.Status)
	}
}

func TestRemoteTaskRuntime_TaskMessages(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	if err := remote.UpsertTask(ctx, taskrt.TaskRecord{
		ID:        "t-msg",
		AgentName: "worker",
		Goal:      "queued",
		Status:    taskrt.TaskRunning,
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	queued, err := remote.EnqueueTaskMessage(ctx, taskrt.TaskMessage{TaskID: "t-msg", Content: "follow-up"})
	if err != nil {
		t.Fatalf("EnqueueTaskMessage: %v", err)
	}
	if queued.ID == "" || queued.CreatedAt.IsZero() {
		t.Fatalf("unexpected queued message: %+v", queued)
	}
	messages, err := remote.ListTaskMessages(ctx, "t-msg", 10)
	if err != nil {
		t.Fatalf("ListTaskMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "follow-up" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	consumed, err := remote.ConsumeTaskMessages(ctx, "t-msg", 10)
	if err != nil {
		t.Fatalf("ConsumeTaskMessages: %v", err)
	}
	if len(consumed) != 1 || consumed[0].Content != "follow-up" {
		t.Fatalf("unexpected consumed messages: %+v", consumed)
	}
	after, err := remote.ListTaskMessages(ctx, "t-msg", 10)
	if err != nil {
		t.Fatalf("ListTaskMessages after consume: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected consumed queue to be empty, got %+v", after)
	}
}

func TestRemoteTaskRuntime_TaskMessagesConsumeWithLimit(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	if err := remote.UpsertTask(ctx, taskrt.TaskRecord{ID: "t-msg-limit", AgentName: "worker", Goal: "queued", Status: taskrt.TaskRunning}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	for _, content := range []string{"m1", "m2", "m3"} {
		if _, err := remote.EnqueueTaskMessage(ctx, taskrt.TaskMessage{TaskID: "t-msg-limit", Content: content}); err != nil {
			t.Fatalf("EnqueueTaskMessage: %v", err)
		}
	}
	consumed, err := remote.ConsumeTaskMessages(ctx, "t-msg-limit", 2)
	if err != nil {
		t.Fatalf("ConsumeTaskMessages: %v", err)
	}
	if len(consumed) != 2 || consumed[0].Content != "m1" || consumed[1].Content != "m2" {
		t.Fatalf("unexpected consumed messages: %+v", consumed)
	}
	remaining, err := remote.ListTaskMessages(ctx, "t-msg-limit", 10)
	if err != nil {
		t.Fatalf("ListTaskMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Content != "m3" {
		t.Fatalf("unexpected remaining messages: %+v", remaining)
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

func TestRemoteTaskRuntime_WithToken(t *testing.T) {
	// server that requires Authorization: Bearer secret
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// serve a fake task record
		_ = json.NewEncoder(w).Encode(taskrt.TaskRecord{ID: "t1", Goal: "ok"})
	}))
	defer ts.Close()

	remote := distributed.NewRemoteTaskRuntime(ts.URL, distributed.WithToken("secret"))
	got, err := remote.GetTask(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "t1" {
		t.Errorf("got id %q, want t1", got.ID)
	}
}

func TestRemoteTaskRuntime_WithHTTPClient(t *testing.T) {
	ts, _ := setupServer()
	defer ts.Close()

	custom := &http.Client{Timeout: 5 * time.Second}
	remote := distributed.NewRemoteTaskRuntime(ts.URL, distributed.WithHTTPClient(custom))
	ctx := context.Background()
	if err := remote.UpsertTask(ctx, taskrt.TaskRecord{ID: "custom-client", Goal: "test", Status: taskrt.TaskPending}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	got, err := remote.GetTask(ctx, "custom-client")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Goal != "test" {
		t.Errorf("got goal %q, want test", got.Goal)
	}
}

func TestRemoteTaskRuntime_ListJobs(t *testing.T) {
	ts, remote := setupServer()
	defer ts.Close()

	ctx := context.Background()
	for _, id := range []string{"j1", "j2", "j3"} {
		if err := remote.UpsertJob(ctx, taskrt.AgentJob{
			ID:        id,
			AgentName: "batch-worker",
			Goal:      "goal-" + id,
			Status:    taskrt.JobPending,
		}); err != nil {
			t.Fatalf("UpsertJob %s: %v", id, err)
		}
	}

	jobs, err := remote.ListJobs(ctx, taskrt.JobQuery{AgentName: "batch-worker"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("got %d jobs, want 3", len(jobs))
	}

	// filter by status
	jobs2, err := remote.ListJobs(ctx, taskrt.JobQuery{Status: taskrt.JobPending})
	if err != nil {
		t.Fatalf("ListJobs by status: %v", err)
	}
	if len(jobs2) != 3 {
		t.Errorf("got %d pending jobs, want 3", len(jobs2))
	}

	// filter with limit
	jobs3, err := remote.ListJobs(ctx, taskrt.JobQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListJobs with limit: %v", err)
	}
	if len(jobs3) != 2 {
		t.Errorf("got %d jobs with limit=2, want 2", len(jobs3))
	}
}
