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
	ID          string
	Description string
	Status      string
	Steps       int
	Error       string
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
		return "等待 manager 委派任务。\n\n这里会显示当前批次的 worker 执行状态。"
	}

	var b strings.Builder
	state := "执行中"
	if t.finished {
		state = "已完成"
	}
	b.WriteString(fmt.Sprintf("状态: %s\n", state))
	b.WriteString(fmt.Sprintf("运行中: %d\n", t.running))
	b.WriteString(fmt.Sprintf("成功: %d\n", t.succeeded))
	b.WriteString(fmt.Sprintf("失败: %d\n", t.failed))
	b.WriteString("\n任务:\n")
	for _, task := range t.tasks {
		icon := "○"
		switch task.Status {
		case "running":
			icon = "⏳"
		case "done":
			icon = "✅"
		case "failed":
			icon = "❌"
		}
		b.WriteString(fmt.Sprintf("%s [%s] %s\n", icon, task.ID, truncate(task.Description, 36)))
		if task.Status == "done" && task.Steps > 0 {
			b.WriteString(fmt.Sprintf("   steps: %d\n", task.Steps))
		}
		if task.Status == "failed" && task.Error != "" {
			b.WriteString(fmt.Sprintf("   err: %s\n", truncate(task.Error, 36)))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (t *orchestrationTracker) notify() {
	if t.refresh != nil {
		t.refresh()
	}
}
