package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	memstore "github.com/mossagents/moss/harness/runtime/memory"
	rstate "github.com/mossagents/moss/harness/runtime/state"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type spyObserver struct {
	events []observe.ExecutionEvent
}

func (s *spyObserver) OnLLMCall(context.Context, observe.LLMCallEvent)      {}
func (s *spyObserver) OnToolCall(context.Context, observe.ToolCallEvent)    {}
func (s *spyObserver) OnApproval(context.Context, io.ApprovalEvent)         {}
func (s *spyObserver) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (s *spyObserver) OnError(context.Context, observe.ErrorEvent)          {}
func (s *spyObserver) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	s.events = append(s.events, e)
}

func TestStateCatalogQueryPagination(t *testing.T) {
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	now := time.Now().UTC()
	first, ok := rstate.StateEntryFromSession(&session.Session{
		ID:        "sess-1",
		Status:    session.StatusCompleted,
		Config:    session.SessionConfig{Goal: "alpha"},
		CreatedAt: now.Add(-2 * time.Minute),
		EndedAt:   now.Add(-2 * time.Minute),
	})
	if !ok {
		t.Fatal("expected visible session entry")
	}
	second, ok := rstate.StateEntryFromTask(taskrt.TaskRecord{
		ID:        "task-1",
		AgentName: "general-purpose",
		Goal:      "beta task",
		Status:    taskrt.TaskPending,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	})
	if !ok {
		t.Fatal("expected task entry")
	}
	if err := catalog.Upsert(first); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}
	if err := catalog.Upsert(second); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}

	page, err := catalog.Query(rstate.StateQuery{Limit: 1})
	if err != nil {
		t.Fatalf("Query page 1: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(page.Items))
	}
	if page.Items[0].RecordID != "task-1" {
		t.Fatalf("expected newest item first, got %q", page.Items[0].RecordID)
	}
	if page.NextCursor == "" {
		t.Fatal("expected next cursor")
	}

	next, err := catalog.Query(rstate.StateQuery{Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("Query page 2: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].RecordID != "sess-1" {
		t.Fatalf("unexpected second page: %+v", next.Items)
	}

	search, err := catalog.Query(rstate.StateQuery{Text: "beta"})
	if err != nil {
		t.Fatalf("Query search: %v", err)
	}
	if len(search.Items) != 1 || search.Items[0].RecordID != "task-1" {
		t.Fatalf("unexpected search result: %+v", search.Items)
	}
}

func TestStateCatalogObserverComposition(t *testing.T) {
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	k := kernel.New(WithStateCatalog(catalog))
	spy := &spyObserver{}
	observer := observe.JoinObservers(spy, ObserverForStateCatalog(k))
	event := observe.ExecutionEvent{
		Type:      observe.ExecutionToolCompleted,
		SessionID: "sess-1",
		ToolName:  "run_command",
		Timestamp: time.Now().UTC(),
	}
	observer.OnExecutionEvent(context.Background(), event)

	if len(spy.events) != 1 {
		t.Fatalf("expected spy observer to receive event, got %d", len(spy.events))
	}
	page, err := catalog.Query(rstate.StateQuery{Kinds: []rstate.StateKind{rstate.StateKindExecutionEvent}})
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 execution event entry, got %d", len(page.Items))
	}
	if page.Items[0].Title == "" || page.Items[0].SessionID != "sess-1" {
		t.Fatalf("unexpected catalog event entry: %+v", page.Items[0])
	}
	if page.Items[0].Kind != rstate.StateKindExecutionEvent {
		t.Fatalf("expected execution_event kind, got %q", page.Items[0].Kind)
	}
}

func TestStateCatalogObserverPersistsHostedToolExecutionEvent(t *testing.T) {
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	k := kernel.New(WithStateCatalog(catalog))
	observer := ObserverForStateCatalog(k)
	event := observe.ExecutionEvent{
		EventID:     "evt-hosted-1",
		Type:        observe.ExecutionHostedToolCompleted,
		SessionID:   "sess-hosted",
		ToolName:    "file_search_call",
		CallID:      "ht-1",
		PayloadKind: "hosted_tool",
		Timestamp:   time.Now().UTC(),
		Metadata:    map[string]any{"status": "completed"},
	}
	observer.OnExecutionEvent(context.Background(), event)

	page, err := catalog.Query(rstate.StateQuery{Kinds: []rstate.StateKind{rstate.StateKindExecutionEvent}})
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 execution event entry, got %d", len(page.Items))
	}
	item := page.Items[0]
	if item.Title != "hosted_tool.completed:file_search_call" {
		t.Fatalf("unexpected hosted tool title: %+v", item)
	}
	var meta map[string]any
	if err := json.Unmarshal(item.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal state metadata: %v", err)
	}
	if payload, ok := meta["payload_kind"]; !ok || payload != "hosted_tool" {
		t.Fatalf("expected payload_kind metadata, got %+v", meta)
	}
	if callID, ok := meta["call_id"]; !ok || callID != "ht-1" {
		t.Fatalf("expected call_id metadata, got %+v", meta)
	}
}

func TestStateEntryFromMemoryAndJobKinds(t *testing.T) {
	now := time.Now().UTC()
	mem, ok := rstate.StateEntryFromMemory(memstore.ExtendedMemoryRecord{
		ID:        "m-1",
		Path:      "team/decision.md",
		Content:   "sqlite backend selected",
		Summary:   "decision",
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	})
	if !ok {
		t.Fatal("expected memory entry")
	}
	if mem.Kind != rstate.StateKindMemory {
		t.Fatalf("expected memory kind, got %q", mem.Kind)
	}

	job, ok := rstate.StateEntryFromJob(taskrt.AgentJob{
		ID:        "job-1",
		AgentName: "worker",
		Goal:      "process data",
		Status:    taskrt.JobRunning,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	})
	if !ok {
		t.Fatal("expected job entry")
	}
	if job.Kind != rstate.StateKindJob {
		t.Fatalf("expected job kind, got %q", job.Kind)
	}

	item, ok := rstate.StateEntryFromJobItem(taskrt.AgentJobItem{
		JobID:     "job-1",
		ItemID:    "item-1",
		Status:    taskrt.JobCompleted,
		Executor:  "agent-a",
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	})
	if !ok {
		t.Fatal("expected job item entry")
	}
	if item.Kind != rstate.StateKindJobItem {
		t.Fatalf("expected job item kind, got %q", item.Kind)
	}
}

func TestIndexedTaskRuntime_JobRuntimeMethods(t *testing.T) {
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	wrapped := rstate.WrapTaskRuntime(taskrt.NewMemoryTaskRuntime(), catalog)
	jobRuntime, ok := wrapped.(taskrt.JobRuntime)
	if !ok {
		t.Fatal("expected wrapped runtime to implement JobRuntime")
	}
	ctx := context.Background()
	if err := jobRuntime.UpsertJob(ctx, taskrt.AgentJob{
		ID:        "job-x",
		AgentName: "worker",
		Goal:      "do work",
		Status:    taskrt.JobPending,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if err := jobRuntime.UpsertJobItem(ctx, taskrt.AgentJobItem{
		JobID:  "job-x",
		ItemID: "item-x",
		Status: taskrt.JobPending,
	}); err != nil {
		t.Fatalf("UpsertJobItem: %v", err)
	}
	page, err := catalog.Query(rstate.StateQuery{Kinds: []rstate.StateKind{rstate.StateKindJob, rstate.StateKindJobItem}})
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if len(page.Items) < 2 {
		t.Fatalf("expected indexed job/job_item entries, got %+v", page.Items)
	}
}

func TestIndexedTaskRuntime_AtomicJobRuntimeMethods(t *testing.T) {
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	wrapped := rstate.WrapTaskRuntime(taskrt.NewMemoryTaskRuntime(), catalog)
	jobRuntime, ok := wrapped.(taskrt.JobRuntime)
	if !ok {
		t.Fatal("expected wrapped runtime to implement JobRuntime")
	}
	atomicRuntime, ok := wrapped.(taskrt.AtomicJobRuntime)
	if !ok {
		t.Fatal("expected wrapped runtime to implement AtomicJobRuntime")
	}
	ctx := context.Background()
	if err := jobRuntime.UpsertJob(ctx, taskrt.AgentJob{
		ID:        "job-atomic",
		AgentName: "worker",
		Goal:      "atomic",
		Status:    taskrt.JobPending,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if _, err := atomicRuntime.MarkJobItemRunning(ctx, "job-atomic", "item-1", "exec-a"); err != nil {
		t.Fatalf("MarkJobItemRunning: %v", err)
	}
	if _, err := atomicRuntime.ReportJobItemResult(ctx, "job-atomic", "item-1", "exec-a", taskrt.JobFailed, "", "boom"); err != nil {
		t.Fatalf("ReportJobItemResult: %v", err)
	}
	page, err := catalog.Query(rstate.StateQuery{Kinds: []rstate.StateKind{rstate.StateKindJobItem}, Text: "item-1"})
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("expected indexed atomic job item entry")
	}
}
