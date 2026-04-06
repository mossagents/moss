package tui

import (
	"path/filepath"
	"strings"
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
			Phase:     "tools", // use tools phase — recorded in transcript; thinking is now skipped
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
	if len(updated.messages) == 0 || updated.messages[len(updated.messages)-1].kind != msgProgress {
		t.Fatalf("expected tools progress appended to transcript, got %+v", updated.messages)
	}
	if !strings.Contains(updated.messages[len(updated.messages)-1].content, "iteration updated") {
		t.Fatalf("unexpected progress transcript content: %+v", updated.messages[len(updated.messages)-1])
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
	if next.visibleProgressHeight() != 0 {
		t.Fatalf("visible progress height = %d, want 0", next.visibleProgressHeight())
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

func TestChatProgressBuildsThinkingTimeline(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.currentSessionID = "s1"
	base := time.Now().UTC()

	updated, _ := m.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "thinking",
			Iteration: 1,
			MaxSteps:  8,
			StartedAt: base,
			UpdatedAt: base,
			Message:   "iteration 1 started",
		},
		SetCurrent: true,
	})
	updated, _ = updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "thinking",
			Iteration: 1,
			MaxSteps:  8,
			StartedAt: base,
			UpdatedAt: base.Add(2 * time.Second),
			Message:   "calling gpt-4o",
		},
	})
	updated, _ = updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "tools",
			Iteration: 1,
			MaxSteps:  8,
			StartedAt: base,
			UpdatedAt: base.Add(4 * time.Second),
			Message:   "running run_command",
			ToolName:  "run_command",
		},
	})

	if len(updated.progressTrail) != 3 {
		t.Fatalf("progress trail length = %d, want 3", len(updated.progressTrail))
	}
	progressCount := 0
	for _, msg := range updated.messages {
		if msg.kind == msgProgress {
			progressCount++
		}
	}
	if progressCount != 1 {
		t.Fatalf("progress transcript count = %d, want 1 (thinking phases no longer added)", progressCount)
	}
	if updated.visibleProgressHeight() != 0 {
		t.Fatalf("visible progress height = %d, want 0", updated.visibleProgressHeight())
	}
	transcript := renderAllMessages(updated.messages, 100, false)
	for _, want := range []string{"running run_command"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("progress transcript missing %q in %q", want, transcript)
		}
	}

	reset, _ := updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s2",
			Status:    "running",
			Phase:     "thinking",
			StartedAt: base.Add(10 * time.Second),
			UpdatedAt: base.Add(10 * time.Second),
			Message:   "iteration 1 started",
		},
		SetCurrent: true,
	})
	if len(reset.progressTrail) != 1 {
		t.Fatalf("expected progress trail reset on session switch, got %d", len(reset.progressTrail))
	}
}

func TestProgressTimelineFreezesElapsedWhenRunCompletes(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.currentSessionID = "s1"
	base := time.Now().UTC()
	current := base
	m.now = func() time.Time { return current }

	updated, _ := m.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "starting",
			StartedAt: base,
			UpdatedAt: base,
			Message:   "run started",
		},
		SetCurrent: true,
	})
	updated, _ = updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "running",
			Phase:     "thinking",
			StartedAt: base,
			UpdatedAt: base.Add(5 * time.Second),
			Message:   "calling gpt-4o",
		},
	})

	// 运行中时 timeline 可见
	current = base.Add(8 * time.Second)
	runningRendered := updated.renderProgressBlock(120)
	if strings.TrimSpace(runningRendered) == "" {
		t.Fatal("expected progress block to be visible while running")
	}

	// 完成后 timeline 不再显示（结果已在 transcript 中）
	completed, _ := updated.Update(notificationProgressMsg{
		Snapshot: executionProgressState{
			SessionID: "s1",
			Status:    "completed",
			Phase:     "completed",
			StartedAt: base,
			UpdatedAt: base.Add(10 * time.Second),
			Message:   "completed in 1 steps",
		},
	})
	current = base.Add(25 * time.Second)
	rendered := completed.renderProgressBlock(120)
	if strings.TrimSpace(rendered) != "" {
		t.Fatalf("expected progress block hidden after run completes, got %q", rendered)
	}
}
