package main

import (
	"fmt"
	"strings"
	"sync"
)

type orchestrationTracker struct {
	mu        sync.RWMutex
	tasks     []trackedTask
	running   int
	succeeded int
	failed    int
	finished  bool
	refresh   func()
}

type trackedTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Steps       int    `json:"steps"`
	Error       string `json:"error,omitempty"`
}

func newOrchestrationTracker(refresh func()) *orchestrationTracker {
	return &orchestrationTracker{refresh: refresh}
}

func (t *orchestrationTracker) StartBatch(tasks []taskInput) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tasks = make([]trackedTask, 0, len(tasks))
	t.running = 0
	t.succeeded = 0
	t.failed = 0
	t.finished = false
	for _, task := range tasks {
		t.tasks = append(t.tasks, trackedTask{
			ID:          task.ID,
			Description: task.Description,
			Status:      "queued",
		})
	}
	t.notify()
}

func (t *orchestrationTracker) StartWorker(task taskInput) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.tasks {
		if t.tasks[i].ID == task.ID {
			if t.tasks[i].Status != "running" {
				t.running++
			}
			t.tasks[i].Status = "running"
			break
		}
	}
	t.notify()
}

func (t *orchestrationTracker) FinishWorker(result taskOutput) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.tasks {
		if t.tasks[i].ID == result.ID {
			if t.tasks[i].Status == "running" && t.running > 0 {
				t.running--
			}
			if result.Success {
				t.tasks[i].Status = "done"
				t.succeeded++
			} else {
				t.tasks[i].Status = "failed"
				t.failed++
			}
			t.tasks[i].Steps = result.Steps
			t.tasks[i].Error = result.Error
			break
		}
	}
	t.notify()
}

func (t *orchestrationTracker) FinishBatch(_ []taskOutput) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finished = true
	t.notify()
}

func (t *orchestrationTracker) Summary() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.tasks) == 0 {
		return ""
	}

	var b strings.Builder
	state := "running"
	if t.finished {
		state = "completed"
	}
	b.WriteString(fmt.Sprintf(`{"state":"%s","running":%d,"succeeded":%d,"failed":%d,"tasks":[`,
		state, t.running, t.succeeded, t.failed))
	for i, task := range t.tasks {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf(`{"id":"%s","description":"%s","status":"%s","steps":%d}`,
			task.ID, escapeJSON(truncate(task.Description, 60)), task.Status, task.Steps))
	}
	b.WriteString("]}")
	return b.String()
}

func (t *orchestrationTracker) notify() {
	if t.refresh != nil {
		t.refresh()
	}
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
