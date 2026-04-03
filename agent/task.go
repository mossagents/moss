package agent

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// TaskStatus 表示异步委派任务的状态。
type TaskStatus = port.TaskStatus

const (
	TaskPending   TaskStatus = port.TaskPending
	TaskRunning   TaskStatus = port.TaskRunning
	TaskCompleted TaskStatus = port.TaskCompleted
	TaskFailed    TaskStatus = port.TaskFailed
	TaskCancelled TaskStatus = port.TaskCancelled
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
	Revision  int64           `json:"revision,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitempty"`
}

// TaskTracker 管理异步委派任务的状态。
type TaskTracker struct {
	mu         sync.RWMutex
	tasks      map[string]*Task
	cancels    map[string]context.CancelFunc
	rev        map[string]int64
	runtime    port.TaskRuntime
	watchers   map[string]map[int64]chan Task
	watcherSeq int64
}

// NewTaskTracker 创建 TaskTracker。
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{
		tasks:    make(map[string]*Task),
		cancels:  make(map[string]context.CancelFunc),
		rev:      make(map[string]int64),
		watchers: make(map[string]map[int64]chan Task),
	}
}

// NewTaskTrackerWithRuntime 创建带 TaskRuntime 镜像的 TaskTracker。
func NewTaskTrackerWithRuntime(runtime port.TaskRuntime) *TaskTracker {
	tt := NewTaskTracker()
	tt.runtime = runtime
	return tt
}

// Add 注册一个新任务。
func (t *TaskTracker) Add(task *Task) {
	t.Start(task, nil)
}

// AddWithCancel 注册一个新任务，并记录其取消函数。
func (t *TaskTracker) AddWithCancel(task *Task, cancel context.CancelFunc) {
	t.Start(task, cancel)
}

// Start 以新的 revision 启动任务，并返回该 revision。
func (t *TaskTracker) Start(task *Task, cancel context.CancelFunc) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := *task
	now := time.Now()
	nextRev := t.rev[task.ID] + 1
	t.rev[task.ID] = nextRev
	cp.Revision = nextRev
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	t.tasks[task.ID] = &cp
	if cancel != nil {
		t.cancels[task.ID] = cancel
	}
	t.mirror(cp)
	t.notifyLocked(cp)
	return nextRev
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

// List 返回满足过滤条件的任务列表（创建时间倒序）。
func (t *TaskTracker) List(filter TaskFilter) []*Task {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Task, 0, len(t.tasks))
	for _, task := range t.tasks {
		if filter.AgentName != "" && task.AgentName != filter.AgentName {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		cp := *task
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i], out[j]
		if ti.CreatedAt.Equal(tj.CreatedAt) {
			return ti.ID < tj.ID
		}
		return ti.CreatedAt.After(tj.CreatedAt)
	})
	return out
}

// Complete 将任务标记为完成。
func (t *TaskTracker) Complete(id, result string, tokens port.TokenUsage) {
	t.completeIf(id, 0, result, tokens)
}

// CompleteIf 在 revision 匹配时将任务标记为完成。
func (t *TaskTracker) CompleteIf(id string, revision int64, result string, tokens port.TokenUsage) {
	t.completeIf(id, revision, result, tokens)
}

func (t *TaskTracker) completeIf(id string, revision int64, result string, tokens port.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if task, ok := t.tasks[id]; ok {
		if revision > 0 && task.Revision != revision {
			return
		}
		if task.Status != TaskRunning {
			return
		}
		task.Status = TaskCompleted
		task.Result = result
		task.Tokens = tokens
		task.UpdatedAt = time.Now()
		delete(t.cancels, id)
		t.mirror(*task)
		t.notifyLocked(*task)
	}
}

// Fail 将任务标记为失败。
func (t *TaskTracker) Fail(id, errMsg string) {
	t.failIf(id, 0, errMsg)
}

// FailIf 在 revision 匹配时将任务标记为失败。
func (t *TaskTracker) FailIf(id string, revision int64, errMsg string) {
	t.failIf(id, revision, errMsg)
}

func (t *TaskTracker) failIf(id string, revision int64, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if task, ok := t.tasks[id]; ok {
		if revision > 0 && task.Revision != revision {
			return
		}
		if task.Status != TaskRunning {
			return
		}
		task.Status = TaskFailed
		task.Error = errMsg
		task.UpdatedAt = time.Now()
		delete(t.cancels, id)
		t.mirror(*task)
		t.notifyLocked(*task)
	}
}

// Cancel 将任务标记为已取消。
func (t *TaskTracker) Cancel(id, errMsg string) {
	t.cancelIf(id, 0, errMsg)
}

// CancelIf 在 revision 匹配时将任务标记为取消并触发对应 cancel 函数。
func (t *TaskTracker) CancelIf(id string, revision int64, errMsg string) {
	t.cancelIf(id, revision, errMsg)
}

func (t *TaskTracker) cancelIf(id string, revision int64, errMsg string) {
	t.mu.Lock()
	task, ok := t.tasks[id]
	cancelFn := t.cancels[id]
	if ok {
		if revision > 0 && task.Revision != revision {
			t.mu.Unlock()
			return
		}
		if task.Status == TaskRunning {
			task.Status = TaskCancelled
			task.Error = errMsg
			task.UpdatedAt = time.Now()
			t.mirror(*task)
			t.notifyLocked(*task)
		}
		delete(t.cancels, id)
	}
	t.mu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}
}

// Subscribe 返回任务状态变更通知通道及取消订阅函数。
func (t *TaskTracker) Subscribe(taskID string) (<-chan Task, func(), error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.tasks[taskID]; !ok {
		return nil, nil, fmt.Errorf("task %q not found", taskID)
	}
	t.watcherSeq++
	watchID := t.watcherSeq
	if _, ok := t.watchers[taskID]; !ok {
		t.watchers[taskID] = make(map[int64]chan Task)
	}
	ch := make(chan Task, 8)
	t.watchers[taskID][watchID] = ch
	cancel := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		byTask, ok := t.watchers[taskID]
		if !ok {
			return
		}
		stream, ok := byTask[watchID]
		if !ok {
			return
		}
		delete(byTask, watchID)
		close(stream)
		if len(byTask) == 0 {
			delete(t.watchers, taskID)
		}
	}
	return ch, cancel, nil
}

func (t *TaskTracker) notifyLocked(task Task) {
	byTask, ok := t.watchers[task.ID]
	if !ok || len(byTask) == 0 {
		return
	}
	for _, ch := range byTask {
		select {
		case ch <- task:
		default:
		}
	}
}

func (t *TaskTracker) mirror(task Task) {
	if t.runtime == nil {
		return
	}
	_ = t.runtime.UpsertTask(context.Background(), port.TaskRecord{
		ID:          task.ID,
		AgentName:   task.AgentName,
		Goal:        task.Goal,
		Status:      port.TaskStatus(task.Status),
		ClaimedBy:   task.AgentName,
		Result:      task.Result,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
		WorkspaceID: "",
	})
}
