package port

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// TaskStatus 表示任务运行时状态。
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// TaskRecord 是跨 agent/runtime 共享的任务记录。
type TaskRecord struct {
	ID          string     `json:"id"`
	AgentName   string     `json:"agent_name"`
	Goal        string     `json:"goal"`
	Status      TaskStatus `json:"status"`
	DependsOn   []string   `json:"depends_on,omitempty"`
	ClaimedBy   string     `json:"claimed_by,omitempty"`
	WorkspaceID string     `json:"workspace_id,omitempty"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
}

// TaskQuery 用于筛选任务。
type TaskQuery struct {
	AgentName string     `json:"agent_name,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
	ClaimedBy string     `json:"claimed_by,omitempty"`
	Limit     int        `json:"limit,omitempty"`
}

// TaskRuntime 提供持久任务图的最小能力。
type TaskRuntime interface {
	UpsertTask(ctx context.Context, task TaskRecord) error
	GetTask(ctx context.Context, id string) (*TaskRecord, error)
	ListTasks(ctx context.Context, query TaskQuery) ([]TaskRecord, error)
	ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*TaskRecord, error)
}

// ErrTaskNotFound 表示任务不存在。
var ErrTaskNotFound = errors.New("task not found")

// ErrNoReadyTask 表示没有可认领任务。
var ErrNoReadyTask = errors.New("no ready task")

// MemoryTaskRuntime 是内存版任务运行时（POC 默认实现）。
type MemoryTaskRuntime struct {
	mu    sync.RWMutex
	tasks map[string]TaskRecord
}

func NewMemoryTaskRuntime() *MemoryTaskRuntime {
	return &MemoryTaskRuntime{
		tasks: make(map[string]TaskRecord),
	}
}

func (r *MemoryTaskRuntime) UpsertTask(_ context.Context, task TaskRecord) error {
	if task.ID == "" {
		return errors.New("task id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	existing, ok := r.tasks[task.ID]
	if ok {
		if task.CreatedAt.IsZero() {
			task.CreatedAt = existing.CreatedAt
		}
	} else if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	task.DependsOn = append([]string(nil), task.DependsOn...)
	r.tasks[task.ID] = task
	return nil
}

func (r *MemoryTaskRuntime) GetTask(_ context.Context, id string) (*TaskRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	cp := task
	cp.DependsOn = append([]string(nil), task.DependsOn...)
	return &cp, nil
}

func (r *MemoryTaskRuntime) ListTasks(_ context.Context, query TaskQuery) ([]TaskRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TaskRecord, 0, len(r.tasks))
	for _, t := range r.tasks {
		if query.AgentName != "" && t.AgentName != query.AgentName {
			continue
		}
		if query.Status != "" && t.Status != query.Status {
			continue
		}
		if query.ClaimedBy != "" && t.ClaimedBy != query.ClaimedBy {
			continue
		}
		cp := t
		cp.DependsOn = append([]string(nil), t.DependsOn...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func (r *MemoryTaskRuntime) ClaimNextReady(_ context.Context, claimer string, preferredAgent string) (*TaskRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var best *TaskRecord
	for _, candidate := range r.tasks {
		if candidate.Status != TaskPending {
			continue
		}
		if preferredAgent != "" && candidate.AgentName != "" && candidate.AgentName != preferredAgent {
			continue
		}
		ready := true
		for _, depID := range candidate.DependsOn {
			dep, ok := r.tasks[depID]
			if !ok || dep.Status != TaskCompleted {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		cp := candidate
		if best == nil || cp.CreatedAt.Before(best.CreatedAt) || (cp.CreatedAt.Equal(best.CreatedAt) && cp.ID < best.ID) {
			best = &cp
		}
	}
	if best == nil {
		return nil, ErrNoReadyTask
	}
	best.Status = TaskRunning
	best.ClaimedBy = claimer
	best.UpdatedAt = time.Now()
	r.tasks[best.ID] = *best
	cp := *best
	cp.DependsOn = append([]string(nil), best.DependsOn...)
	return &cp, nil
}

