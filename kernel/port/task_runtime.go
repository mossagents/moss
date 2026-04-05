package port

import (
	"context"
	"errors"
	"sort"
	"strings"
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
	ID              string     `json:"id"`
	AgentName       string     `json:"agent_name"`
	Goal            string     `json:"goal"`
	Status          TaskStatus `json:"status"`
	DependsOn       []string   `json:"depends_on,omitempty"`
	ClaimedBy       string     `json:"claimed_by,omitempty"`
	WorkspaceID     string     `json:"workspace_id,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	ParentSessionID string     `json:"parent_session_id,omitempty"`
	JobID           string     `json:"job_id,omitempty"`
	JobItemID       string     `json:"job_item_id,omitempty"`
	Result          string     `json:"result,omitempty"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       time.Time  `json:"created_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
}

// AgentJobStatus 表示 Job/Item 状态机状态。
type AgentJobStatus string

const (
	JobPending   AgentJobStatus = "pending"
	JobRunning   AgentJobStatus = "running"
	JobCompleted AgentJobStatus = "completed"
	JobFailed    AgentJobStatus = "failed"
	JobCancelled AgentJobStatus = "cancelled"
)

// AgentJob 表示一个可恢复的作业级任务。
type AgentJob struct {
	ID        string         `json:"id"`
	AgentName string         `json:"agent_name"`
	Goal      string         `json:"goal"`
	Status    AgentJobStatus `json:"status"`
	Revision  int64          `json:"revision,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

// AgentJobItem 表示 Job 下的子项执行状态。
type AgentJobItem struct {
	JobID        string         `json:"job_id"`
	ItemID       string         `json:"item_id"`
	Status       AgentJobStatus `json:"status"`
	Executor     string         `json:"executor,omitempty"`
	AttemptCount int            `json:"attempt_count,omitempty"`
	ReportedAt   time.Time      `json:"reported_at,omitempty"`
	Result       string         `json:"result,omitempty"`
	Error        string         `json:"error,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
}

// TaskQuery 用于筛选任务。
type TaskQuery struct {
	AgentName string     `json:"agent_name,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
	ClaimedBy string     `json:"claimed_by,omitempty"`
	Limit     int        `json:"limit,omitempty"`
}

// JobQuery 用于筛选 Job。
type JobQuery struct {
	AgentName string         `json:"agent_name,omitempty"`
	Status    AgentJobStatus `json:"status,omitempty"`
	Limit     int            `json:"limit,omitempty"`
}

// JobItemQuery 用于筛选 JobItem。
type JobItemQuery struct {
	JobID  string         `json:"job_id"`
	Status AgentJobStatus `json:"status,omitempty"`
	Limit  int            `json:"limit,omitempty"`
}

// TaskRuntime 提供持久任务图的最小能力。
type TaskRuntime interface {
	UpsertTask(ctx context.Context, task TaskRecord) error
	GetTask(ctx context.Context, id string) (*TaskRecord, error)
	ListTasks(ctx context.Context, query TaskQuery) ([]TaskRecord, error)
	ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*TaskRecord, error)
}

// JobRuntime 提供 Job/Item 双层状态机能力。
type JobRuntime interface {
	UpsertJob(ctx context.Context, job AgentJob) error
	GetJob(ctx context.Context, id string) (*AgentJob, error)
	ListJobs(ctx context.Context, query JobQuery) ([]AgentJob, error)
	UpsertJobItem(ctx context.Context, item AgentJobItem) error
	ListJobItems(ctx context.Context, query JobItemQuery) ([]AgentJobItem, error)
}

// AtomicJobRuntime 提供带执行者绑定的原子 item 更新语义。
type AtomicJobRuntime interface {
	MarkJobItemRunning(ctx context.Context, jobID, itemID, executor string) (*AgentJobItem, error)
	ReportJobItemResult(ctx context.Context, jobID, itemID, executor string, status AgentJobStatus, result string, errMsg string) (*AgentJobItem, error)
}

// ErrTaskNotFound 表示任务不存在。
var ErrTaskNotFound = errors.New("task not found")

// ErrNoReadyTask 表示没有可认领任务。
var ErrNoReadyTask = errors.New("no ready task")

// ErrJobNotFound 表示 job 不存在。
var ErrJobNotFound = errors.New("job not found")

// ErrInvalidJobTransition 表示 job/item 状态迁移非法。
var ErrInvalidJobTransition = errors.New("invalid job state transition")

// ErrJobItemExecutorMismatch 表示 item 结果回报与当前执行者不匹配（迟到或串扰）。
var ErrJobItemExecutorMismatch = errors.New("job item executor mismatch")

// ErrJobItemNotFound 表示 job item 不存在。
var ErrJobItemNotFound = errors.New("job item not found")

// MemoryTaskRuntime 是内存版任务运行时（POC 默认实现）。
type MemoryTaskRuntime struct {
	mu    sync.RWMutex
	tasks map[string]TaskRecord
	jobs  map[string]AgentJob
	items map[string]map[string]AgentJobItem
}

func NewMemoryTaskRuntime() *MemoryTaskRuntime {
	return &MemoryTaskRuntime{
		tasks: make(map[string]TaskRecord),
		jobs:  make(map[string]AgentJob),
		items: make(map[string]map[string]AgentJobItem),
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

func (r *MemoryTaskRuntime) UpsertJob(_ context.Context, job AgentJob) error {
	if job.ID == "" {
		return errors.New("job id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	existing, ok := r.jobs[job.ID]
	if ok {
		if job.CreatedAt.IsZero() {
			job.CreatedAt = existing.CreatedAt
		}
		if !canTransitionJobStatus(existing.Status, job.Status) {
			return ErrInvalidJobTransition
		}
		job.Revision = existing.Revision + 1
	} else {
		if job.CreatedAt.IsZero() {
			job.CreatedAt = now
		}
		if job.Status == "" {
			job.Status = JobPending
		}
		job.Revision = 1
	}
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return nil
}

func (r *MemoryTaskRuntime) GetJob(_ context.Context, id string) (*AgentJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	cp := job
	return &cp, nil
}

func (r *MemoryTaskRuntime) ListJobs(_ context.Context, query JobQuery) ([]AgentJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentJob, 0, len(r.jobs))
	for _, j := range r.jobs {
		if query.AgentName != "" && j.AgentName != query.AgentName {
			continue
		}
		if query.Status != "" && j.Status != query.Status {
			continue
		}
		out = append(out, j)
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

func (r *MemoryTaskRuntime) UpsertJobItem(_ context.Context, item AgentJobItem) error {
	if item.JobID == "" || item.ItemID == "" {
		return errors.New("job_id and item_id are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[item.JobID]; !ok {
		return ErrJobNotFound
	}
	if _, ok := r.items[item.JobID]; !ok {
		r.items[item.JobID] = make(map[string]AgentJobItem)
	}
	now := time.Now()
	existing, ok := r.items[item.JobID][item.ItemID]
	if ok {
		if !canTransitionJobStatus(existing.Status, item.Status) {
			return ErrInvalidJobTransition
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = existing.CreatedAt
		}
	} else if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.Status == "" {
		item.Status = JobPending
	}
	item.UpdatedAt = now
	r.items[item.JobID][item.ItemID] = item
	return nil
}

func (r *MemoryTaskRuntime) MarkJobItemRunning(_ context.Context, jobID, itemID, executor string) (*AgentJobItem, error) {
	if jobID == "" || itemID == "" {
		return nil, errors.New("job_id and item_id are required")
	}
	executor = strings.TrimSpace(executor)
	if executor == "" {
		return nil, errors.New("executor is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[jobID]; !ok {
		return nil, ErrJobNotFound
	}
	if _, ok := r.items[jobID]; !ok {
		r.items[jobID] = make(map[string]AgentJobItem)
	}
	now := time.Now()
	item, ok := r.items[jobID][itemID]
	if !ok {
		item = AgentJobItem{
			JobID:     jobID,
			ItemID:    itemID,
			Status:    JobPending,
			CreatedAt: now,
		}
	}
	if !canTransitionJobStatus(item.Status, JobRunning) {
		return nil, ErrInvalidJobTransition
	}
	item.Status = JobRunning
	item.Executor = executor
	item.AttemptCount++
	item.Error = ""
	item.Result = ""
	item.ReportedAt = time.Time{}
	item.UpdatedAt = now
	r.items[jobID][itemID] = item
	cp := item
	return &cp, nil
}

func (r *MemoryTaskRuntime) ReportJobItemResult(_ context.Context, jobID, itemID, executor string, status AgentJobStatus, result string, errMsg string) (*AgentJobItem, error) {
	if jobID == "" || itemID == "" {
		return nil, errors.New("job_id and item_id are required")
	}
	executor = strings.TrimSpace(executor)
	if executor == "" {
		return nil, errors.New("executor is required")
	}
	if status != JobCompleted && status != JobFailed && status != JobCancelled {
		return nil, ErrInvalidJobTransition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[jobID]; !ok {
		return nil, ErrJobNotFound
	}
	byJob, ok := r.items[jobID]
	if !ok {
		return nil, ErrJobItemNotFound
	}
	item, ok := byJob[itemID]
	if !ok {
		return nil, ErrJobItemNotFound
	}
	if item.Executor != executor || item.Status != JobRunning {
		return nil, ErrJobItemExecutorMismatch
	}
	if !canTransitionJobStatus(item.Status, status) {
		return nil, ErrInvalidJobTransition
	}
	now := time.Now()
	item.Status = status
	item.Result = result
	item.Error = errMsg
	item.ReportedAt = now
	item.UpdatedAt = now
	byJob[itemID] = item
	cp := item
	return &cp, nil
}

func (r *MemoryTaskRuntime) ListJobItems(_ context.Context, query JobItemQuery) ([]AgentJobItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if query.JobID == "" {
		return nil, errors.New("job_id is required")
	}
	byJob, ok := r.items[query.JobID]
	if !ok {
		return []AgentJobItem{}, nil
	}
	out := make([]AgentJobItem, 0, len(byJob))
	for _, item := range byJob {
		if query.Status != "" && item.Status != query.Status {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ItemID < out[j].ItemID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func canTransitionJobStatus(from, to AgentJobStatus) bool {
	if from == "" || from == to {
		return true
	}
	switch from {
	case JobPending:
		return to == JobRunning || to == JobCancelled
	case JobRunning:
		return to == JobCompleted || to == JobFailed || to == JobCancelled
	case JobFailed, JobCancelled:
		return to == JobRunning
	case JobCompleted:
		return false
	default:
		return false
	}
}
