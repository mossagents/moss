package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func TestChatProgressIgnoresOtherSessions(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.currentSessionID = "s1"
	now := time.Now().UTC()
	updated, _ := m.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "thinking",
			Iteration: 2,
			MaxSteps:  10,
			StartedAt: now.Add(-5 * time.Second),
			UpdatedAt: now,
			Message:   "iteration updated",
		},
	})
	if updated.progress.SessionID != "s1" {
		t.Fatalf("progress session = %q, want s1", updated.progress.SessionID)
	}
	next, _ := updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s2",
			Status:    "running",
			Message:   "other session",
			UpdatedAt: now,
		},
	})
	if next.progress.SessionID != "s1" {
		t.Fatalf("unexpected progress replacement from other session: %+v", next.progress)
	}
	if next.visibleProgressHeight() != 1 {
		t.Fatalf("visible progress height = %d, want 1", next.visibleProgressHeight())
	}
}

func TestRebuildExecutionProgressUsesLatestRun(t *testing.T) {
	dir := t.TempDir()
	catalog, err := runtime.NewStateCatalog(filepath.Join(dir, "catalog"), filepath.Join(dir, "events"), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	sess := &session.Session{
		ID:     "s-replay",
		Budget: session.Budget{MaxSteps: 12},
	}
	base := time.Now().UTC().Add(-time.Minute)
	events := []port.ExecutionEvent{
		{
			Type:      port.ExecutionRunStarted,
			SessionID: sess.ID,
			Timestamp: base,
			Data:      map[string]any{"max_steps": 12},
		},
		{
			Type:      port.ExecutionIterationStarted,
			SessionID: sess.ID,
			Timestamp: base.Add(1 * time.Second),
			Data:      map[string]any{"iteration": 1, "max_steps": 12},
		},
		{
			Type:      port.ExecutionRunCompleted,
			SessionID: sess.ID,
			Timestamp: base.Add(2 * time.Second),
			Data:      map[string]any{"steps": 1, "tokens": 20},
		},
		{
			Type:      port.ExecutionRunStarted,
			SessionID: sess.ID,
			Timestamp: base.Add(3 * time.Second),
			Data:      map[string]any{"max_steps": 12},
		},
		{
			Type:      port.ExecutionIterationStarted,
			SessionID: sess.ID,
			Timestamp: base.Add(4 * time.Second),
			Data:      map[string]any{"iteration": 2, "max_steps": 12},
		},
		{
			Type:      port.ExecutionToolStarted,
			SessionID: sess.ID,
			Timestamp: base.Add(5 * time.Second),
			ToolName:  "run_command",
		},
	}
	for _, event := range events {
		if err := catalog.AppendExecutionEvent(event); err != nil {
			t.Fatalf("AppendExecutionEvent(%s): %v", event.Type, err)
		}
	}
	state := rebuildExecutionProgress(catalog, sess)
	if state.Status != "running" {
		t.Fatalf("status = %q, want running", state.Status)
	}
	if state.Phase != "tools" {
		t.Fatalf("phase = %q, want tools", state.Phase)
	}
	if state.ToolName != "run_command" {
		t.Fatalf("tool = %q, want run_command", state.ToolName)
	}
	if state.Iteration != 2 {
		t.Fatalf("iteration = %d, want 2", state.Iteration)
	}
}
