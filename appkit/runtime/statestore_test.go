package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

type spyObserver struct {
	events []port.ExecutionEvent
}

func (s *spyObserver) OnLLMCall(context.Context, port.LLMCallEvent)      {}
func (s *spyObserver) OnToolCall(context.Context, port.ToolCallEvent)    {}
func (s *spyObserver) OnApproval(context.Context, port.ApprovalEvent)    {}
func (s *spyObserver) OnSessionEvent(context.Context, port.SessionEvent) {}
func (s *spyObserver) OnError(context.Context, port.ErrorEvent)          {}
func (s *spyObserver) OnExecutionEvent(_ context.Context, e port.ExecutionEvent) {
	s.events = append(s.events, e)
}

func TestStateCatalogQueryPagination(t *testing.T) {
	catalog, err := NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	now := time.Now().UTC()
	first, ok := StateEntryFromSession(&session.Session{
		ID:        "sess-1",
		Status:    session.StatusCompleted,
		Config:    session.SessionConfig{Goal: "alpha"},
		CreatedAt: now.Add(-2 * time.Minute),
		EndedAt:   now.Add(-2 * time.Minute),
	})
	if !ok {
		t.Fatal("expected visible session entry")
	}
	second, ok := StateEntryFromTask(port.TaskRecord{
		ID:        "task-1",
		AgentName: "general-purpose",
		Goal:      "beta task",
		Status:    port.TaskPending,
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

	page, err := catalog.Query(StateQuery{Limit: 1})
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

	next, err := catalog.Query(StateQuery{Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("Query page 2: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].RecordID != "sess-1" {
		t.Fatalf("unexpected second page: %+v", next.Items)
	}

	search, err := catalog.Query(StateQuery{Text: "beta"})
	if err != nil {
		t.Fatalf("Query search: %v", err)
	}
	if len(search.Items) != 1 || search.Items[0].RecordID != "task-1" {
		t.Fatalf("unexpected search result: %+v", search.Items)
	}
}

func TestStateCatalogObserverComposition(t *testing.T) {
	catalog, err := NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	k := kernel.New(WithStateCatalog(catalog))
	spy := &spyObserver{}
	observer := port.JoinObservers(spy, ObserverForStateCatalog(k))
	event := port.ExecutionEvent{
		Type:      port.ExecutionToolCompleted,
		SessionID: "sess-1",
		ToolName:  "run_command",
		Timestamp: time.Now().UTC(),
	}
	observer.OnExecutionEvent(context.Background(), event)

	if len(spy.events) != 1 {
		t.Fatalf("expected spy observer to receive event, got %d", len(spy.events))
	}
	page, err := catalog.Query(StateQuery{Kinds: []StateKind{StateKindExecutionEvent}})
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 execution event entry, got %d", len(page.Items))
	}
	if page.Items[0].Title == "" || page.Items[0].SessionID != "sess-1" {
		t.Fatalf("unexpected catalog event entry: %+v", page.Items[0])
	}
	if page.Items[0].Kind != StateKindExecutionEvent {
		t.Fatalf("expected execution_event kind, got %q", page.Items[0].Kind)
	}
}
