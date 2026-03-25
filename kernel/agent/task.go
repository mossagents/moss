package agent

import (
	"sync"

	"github.com/mossagi/moss/kernel/port"
)

// TaskStatus 表示异步委派任务的状态。
type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task 表示一个异步委派任务。
type Task struct {
	ID        string          `json:"id"`
	AgentName string          `json:"agent_name"`
	Goal      string          `json:"goal"`
	Status    TaskStatus      `json:"status"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Tokens    port.TokenUsage `json:"tokens,omitempty"`
}

// TaskTracker 管理异步委派任务的状态。
type TaskTracker struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewTaskTracker 创建 TaskTracker。
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{tasks: make(map[string]*Task)}
}

// Add 注册一个新任务。
func (t *TaskTracker) Add(task *Task) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := *task
	t.tasks[task.ID] = &cp
}

// Get 按 ID 查找任务。
func (t *TaskTracker) Get(id string) (*Task, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	task, ok := t.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *task
	return &cp, true
}

// Complete 将任务标记为完成。
func (t *TaskTracker) Complete(id, result string, tokens port.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if task, ok := t.tasks[id]; ok {
		task.Status = TaskCompleted
		task.Result = result
		task.Tokens = tokens
	}
}

// Fail 将任务标记为失败。
func (t *TaskTracker) Fail(id, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if task, ok := t.tasks[id]; ok {
		task.Status = TaskFailed
		task.Error = errMsg
	}
}

// Cancel 将任务标记为已取消。
func (t *TaskTracker) Cancel(id, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if task, ok := t.tasks[id]; ok {
		task.Status = TaskCancelled
		task.Error = errMsg
	}
}
