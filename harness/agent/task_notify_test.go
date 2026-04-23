package agent

import (
	"context"
	"testing"
	"time"
)

// TestNotifyLocked_DrainAndRetry verifies that when a watcher channel is full,
// notifyLocked drains the oldest item and re-sends the latest state so that
// terminal updates are never silently lost.
func TestNotifyLocked_DrainAndRetry(t *testing.T) {
	tracker := &TaskTracker{
		tasks:    make(map[string]*Task),
		cancels:  make(map[string]context.CancelFunc),
		rev:      make(map[string]int64),
		watchers: make(map[string]map[int64]chan Task),
	}

	const taskID = "test-task"
	tracker.tasks[taskID] = &Task{ID: taskID, Status: TaskRunning}

	// Create a watcher channel with capacity 1 and pre-fill it with a stale item.
	ch := make(chan Task, 1)
	stale := Task{ID: taskID, Status: TaskRunning}
	ch <- stale

	tracker.watchers[taskID] = map[int64]chan Task{1: ch}

	// Send a terminal update while the channel is full.
	terminal := Task{ID: taskID, Status: TaskCompleted}
	tracker.notifyLocked(terminal)

	// The channel must now contain the terminal state, not the stale one.
	select {
	case got := <-ch:
		if got.Status != TaskCompleted {
			t.Errorf("expected TaskCompleted, got %v", got.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel was empty after notifyLocked — terminal state was lost")
	}
}
