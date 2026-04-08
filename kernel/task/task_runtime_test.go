package task

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

func TestMemoryTaskRuntime_AtomicJobItemExecutorBinding(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job-atomic", AgentName: "worker", Goal: "atomic", Status: JobPending}); err != nil {
		t.Fatal(err)
	}
	item, err := rt.MarkJobItemRunning(ctx, "job-atomic", "item-1", "exec-a")
	if err != nil {
		t.Fatal(err)
	}
	if item.AttemptCount != 1 || item.Executor != "exec-a" || item.Status != JobRunning {
		t.Fatalf("unexpected running item: %+v", item)
	}
	if _, err := rt.ReportJobItemResult(ctx, "job-atomic", "item-1", "exec-b", JobCompleted, "late", ""); !errors.Is(err, ErrJobItemExecutorMismatch) {
		t.Fatalf("expected ErrJobItemExecutorMismatch, got %v", err)
	}
	reported, err := rt.ReportJobItemResult(ctx, "job-atomic", "item-1", "exec-a", JobFailed, "", "boom")
	if err != nil {
		t.Fatal(err)
	}
	if reported.Status != JobFailed || reported.Error != "boom" || reported.ReportedAt.IsZero() {
		t.Fatalf("unexpected reported item: %+v", reported)
	}
	restarted, err := rt.MarkJobItemRunning(ctx, "job-atomic", "item-1", "exec-a")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.AttemptCount != 2 {
		t.Fatalf("expected attempt_count=2, got %+v", restarted)
	}
}

func TestFileTaskRuntime_AtomicJobItemExecutorBindingPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jobs-atomic")
	ctx := context.Background()
	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJob(ctx, AgentJob{ID: "job-persist", AgentName: "worker", Goal: "persist", Status: JobPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.MarkJobItemRunning(ctx, "job-persist", "item-1", "exec-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.ReportJobItemResult(ctx, "job-persist", "item-1", "exec-a", JobFailed, "", "boom"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reloaded.ListJobItems(ctx, JobItemQuery{JobID: "job-persist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected item count: %+v", items)
	}
	if items[0].AttemptCount != 1 || items[0].Executor != "exec-a" || items[0].Status != JobFailed || items[0].ReportedAt.IsZero() {
		t.Fatalf("unexpected persisted atomic item: %+v", items[0])
	}
}

func TestMemoryTaskRuntime_TaskSummariesAndRelations(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()
	if err := rt.UpsertTask(ctx, TaskRecord{
		ID:              "task-1",
		AgentName:       "worker",
		Goal:            "inspect repo",
		Status:          TaskPending,
		DependsOn:       []string{"task-0"},
		SessionID:       "sess-1",
		ParentSessionID: "sess-root",
		JobID:           "job-1",
		JobItemID:       "item-1",
	}); err != nil {
		t.Fatal(err)
	}

	graph, ok := any(rt).(TaskGraphRuntime)
	if !ok {
		t.Fatal("memory task runtime should implement TaskGraphRuntime")
	}
	summaries, err := graph.ListTaskSummaries(ctx, TaskQuery{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Handle.SessionID != "sess-1" || summary.Handle.ParentSessionID != "sess-root" || summary.Handle.JobID != "job-1" {
		t.Fatalf("unexpected handle: %+v", summary.Handle)
	}
	if len(summary.Relations) != 5 {
		t.Fatalf("expected dependency/session/parent/job/job_item relations, got %+v", summary.Relations)
	}
	relations, err := graph.ListTaskRelations(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != len(summary.Relations) {
		t.Fatalf("relation count mismatch: %+v vs %+v", relations, summary.Relations)
	}
}

func TestFileTaskRuntime_TaskSummaryQueriesPersist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-summary")
	ctx := context.Background()
	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{
		ID:        "task-a",
		AgentName: "worker",
		Goal:      "resume thread",
		Status:    TaskPending,
		SessionID: "sess-a",
		JobID:     "job-a",
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	graph, ok := any(reloaded).(TaskGraphRuntime)
	if !ok {
		t.Fatal("file task runtime should implement TaskGraphRuntime")
	}
	summaries, err := graph.ListTaskSummaries(ctx, TaskQuery{SessionID: "sess-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Handle.JobID != "job-a" {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
}

func TestMemoryTaskRuntime_TaskMessages(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "task-msg", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	queued, err := rt.EnqueueTaskMessage(ctx, TaskMessage{TaskID: "task-msg", Content: "follow up"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.ID == "" || queued.CreatedAt.IsZero() {
		t.Fatalf("unexpected queued message: %+v", queued)
	}
	list, err := rt.ListTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Content != "follow up" {
		t.Fatalf("unexpected messages: %+v", list)
	}
	consumed, err := rt.ConsumeTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != 1 || consumed[0].Content != "follow up" {
		t.Fatalf("unexpected consumed messages: %+v", consumed)
	}
	after, err := rt.ListTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("expected consumed queue to be empty, got %+v", after)
	}
}

func TestFileTaskRuntime_TaskMessagesPersistAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-messages")
	ctx := context.Background()
	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "task-msg", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EnqueueTaskMessage(ctx, TaskMessage{TaskID: "task-msg", Content: "persist me"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := reloaded.ListTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Content != "persist me" {
		t.Fatalf("unexpected persisted messages: %+v", list)
	}
	consumed, err := reloaded.ConsumeTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != 1 || consumed[0].Content != "persist me" {
		t.Fatalf("unexpected consumed persisted messages: %+v", consumed)
	}
	reloadedAgain, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reloadedAgain.ListTaskMessages(ctx, "task-msg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("expected consumed persisted queue to be empty, got %+v", after)
	}
}

func TestMemoryTaskRuntime_TaskMessagesConsumeWithLimitKeepsRemainder(t *testing.T) {
	rt := NewMemoryTaskRuntime()
	ctx := context.Background()
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "task-msg-limit", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"m1", "m2", "m3"} {
		if _, err := rt.EnqueueTaskMessage(ctx, TaskMessage{TaskID: "task-msg-limit", Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	consumed, err := rt.ConsumeTaskMessages(ctx, "task-msg-limit", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != 2 || consumed[0].Content != "m1" || consumed[1].Content != "m2" {
		t.Fatalf("unexpected consumed messages: %+v", consumed)
	}
	left, err := rt.ListTaskMessages(ctx, "task-msg-limit", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Content != "m3" {
		t.Fatalf("unexpected remaining messages: %+v", left)
	}
}

func TestFileTaskRuntime_TaskMessagesConsumeWithLimitPersistsRemainder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-messages-limit")
	ctx := context.Background()
	rt, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(ctx, TaskRecord{ID: "task-msg-limit", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"m1", "m2", "m3"} {
		if _, err := rt.EnqueueTaskMessage(ctx, TaskMessage{TaskID: "task-msg-limit", Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	consumed, err := rt.ConsumeTaskMessages(ctx, "task-msg-limit", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != 2 || consumed[0].Content != "m1" || consumed[1].Content != "m2" {
		t.Fatalf("unexpected consumed messages: %+v", consumed)
	}
	reloaded, err := NewFileTaskRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	left, err := reloaded.ListTaskMessages(ctx, "task-msg-limit", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Content != "m3" {
		t.Fatalf("unexpected persisted remaining messages: %+v", left)
	}
}
