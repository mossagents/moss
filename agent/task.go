package agent

import (
	"context"
	"fmt"
	"github.com/mossagents/moss/kernel/model"
	taskrt "github.com/mossagents/moss/kernel/task"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TaskStatus 表示异步委派任务的状态。
type TaskStatus = taskrt.TaskStatus

const (
	TaskPending   TaskStatus = taskrt.TaskPending
	TaskRunning   TaskStatus = taskrt.TaskRunning
	TaskCompleted TaskStatus = taskrt.TaskCompleted
	TaskFailed    TaskStatus = taskrt.TaskFailed
	TaskCancelled TaskStatus = taskrt.TaskCancelled
)

// Task 表示一个异步委派任务。
type Task struct {
	ID              string              `json:"id"`
	AgentName       string              `json:"agent_name"`
	Goal            string              `json:"goal"`
	Status          TaskStatus          `json:"status"`
	Contract        taskrt.TaskContract `json:"contract,omitempty"`
	Active          bool                `json:"active,omitempty"`
	SessionID       string              `json:"session_id,omitempty"`
	ParentSessionID string              `json:"parent_session_id,omitempty"`
	WorkspaceID     string              `json:"workspace_id,omitempty"`
	JobID           string              `json:"job_id,omitempty"`
	JobItemID       string              `json:"job_item_id,omitempty"`
	Result          string              `json:"result,omitempty"`
	Error           string              `json:"error,omitempty"`
	Tokens          model.TokenUsage      `json:"tokens,omitempty"`
	Revision        int64               `json:"revision,omitempty"`
	CreatedAt       time.Time           `json:"created_at,omitempty"`
	UpdatedAt       time.Time           `json:"updated_at,omitempty"`
}

// TaskTracker 管理异步委派任务的状态。
type TaskTracker struct {
	mu         sync.RWMutex
	tasks      map[string]*Task
	cancels    map[string]context.CancelFunc
	rev        map[string]int64
	store      TaskStore
	runtime    taskrt.TaskRuntime
	watchers   map[string]map[int64]chan Task
	watcherSeq int64
}

// TaskTrackerOption configures a TaskTracker.
type TaskTrackerOption func(*TaskTracker)

// WithTaskStore sets a custom TaskStore for persistence.
func WithTaskStore(store TaskStore) TaskTrackerOption {
	return func(t *TaskTracker) {
		t.store = store
	}
}

// WithTaskRuntime sets a TaskRuntime for distributed mirroring.
func WithTaskRuntime(runtime taskrt.TaskRuntime) TaskTrackerOption {
	return func(t *TaskTracker) {
		t.runtime = runtime
	}
}

// NewTaskTracker creates a TaskTracker with the given options.
// Defaults to MemoryTaskStore if no store is provided.
func NewTaskTracker(opts ...TaskTrackerOption) *TaskTracker {
	tt := &TaskTracker{
		tasks:    make(map[string]*Task),
		cancels:  make(map[string]context.CancelFunc),
		rev:      make(map[string]int64),
		watchers: make(map[string]map[int64]chan Task),
	}
	for _, opt := range opts {
		opt(tt)
	}
	if tt.store == nil {
		tt.store = NewMemoryTaskStore()
	}
	tt.restoreFromStore(context.Background())
	tt.hydrate(context.Background())
	return tt
}

// NewTaskTrackerWithRuntime 创建带 TaskRuntime 镜像的 TaskTracker。
func NewTaskTrackerWithRuntime(runtime taskrt.TaskRuntime) *TaskTracker {
	return NewTaskTracker(WithTaskRuntime(runtime))
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
	cp := *task
	now := time.Now()
	existing := t.tasks[task.ID]
	nextRev := t.rev[task.ID] + 1
	t.rev[task.ID] = nextRev
	cp.Revision = nextRev
	assignTaskJobIDs(&cp, existing, nextRev)
	cp.Active = cp.Status == TaskRunning && cancel != nil
	if cp.CreatedAt.IsZero() {
		if existing != nil && !existing.CreatedAt.IsZero() {
			cp.CreatedAt = existing.CreatedAt
		} else {
			cp.CreatedAt = now
		}
	}
	cp.UpdatedAt = now
	t.tasks[task.ID] = &cp
	if cancel != nil {
		t.cancels[task.ID] = cancel
	} else {
		delete(t.cancels, task.ID)
	}
	t.notifyLocked(cp)
	t.mu.Unlock()
	t.persist(&cp)
	t.mirror(cp)
	return nextRev
}

func (t *TaskTracker) BindSession(id string, revision int64, sessionID string) {
	t.mu.Lock()
	task, ok := t.tasks[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	if revision > 0 && task.Revision != revision {
		t.mu.Unlock()
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || task.SessionID == sessionID {
		t.mu.Unlock()
		return
	}
	task.SessionID = sessionID
	task.UpdatedAt = time.Now()
	cp := *task
	t.notifyLocked(cp)
	t.mu.Unlock()
	t.persist(&cp)
	t.mirror(cp)
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
func (t *TaskTracker) Complete(id, result string, tokens model.TokenUsage) {
	t.completeIf(id, 0, result, tokens)
}

// CompleteIf 在 revision 匹配时将任务标记为完成。
func (t *TaskTracker) CompleteIf(id string, revision int64, result string, tokens model.TokenUsage) {
	t.completeIf(id, revision, result, tokens)
}

func (t *TaskTracker) completeIf(id string, revision int64, result string, tokens model.TokenUsage) {
	t.mu.Lock()
	var mirrored *Task
	if task, ok := t.tasks[id]; ok {
		if revision > 0 && task.Revision != revision {
			t.mu.Unlock()
			return
		}
		if task.Status != TaskRunning {
			t.mu.Unlock()
			return
		}
		task.Status = TaskCompleted
		task.Active = false
		task.Result = result
		task.Tokens = tokens
		task.UpdatedAt = time.Now()
		delete(t.cancels, id)
		cp := *task
		mirrored = &cp
		t.notifyLocked(cp)
	}
	t.mu.Unlock()
	if mirrored != nil {
		t.persist(mirrored)
		t.mirror(*mirrored)
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
	var mirrored *Task
	if task, ok := t.tasks[id]; ok {
		if revision > 0 && task.Revision != revision {
			t.mu.Unlock()
			return
		}
		if task.Status != TaskRunning {
			t.mu.Unlock()
			return
		}
		task.Status = TaskFailed
		task.Active = false
		task.Error = errMsg
		task.UpdatedAt = time.Now()
		delete(t.cancels, id)
		cp := *task
		mirrored = &cp
		t.notifyLocked(cp)
	}
	t.mu.Unlock()
	if mirrored != nil {
		t.persist(mirrored)
		t.mirror(*mirrored)
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
	var mirrored *Task
	if ok {
		if revision > 0 && task.Revision != revision {
			t.mu.Unlock()
			return
		}
		if task.Status == TaskRunning {
			task.Status = TaskCancelled
			task.Active = false
			task.Error = errMsg
			task.UpdatedAt = time.Now()
			cp := *task
			mirrored = &cp
			t.notifyLocked(cp)
		}
		delete(t.cancels, id)
	}
	t.mu.Unlock()
	if mirrored != nil {
		t.persist(mirrored)
		t.mirror(*mirrored)
	}
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
			// Channel buffer full — drain the oldest (stale) item and retry so
			// the latest state (especially terminal states) always gets delivered.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- task:
			default:
			}
		}
	}
	// 任务达到终态时自动关闭所有 watcher，防止无限增长。
	if task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskCancelled {
		for watchID, ch := range byTask {
			close(ch)
			delete(byTask, watchID)
		}
		delete(t.watchers, task.ID)
	}
}

// CleanupWatchers 清除所有已终态任务的残留 watcher。
// 可在维护周期中调用，防止 watcher map 无限增长。
func (t *TaskTracker) CleanupWatchers() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for taskID, byTask := range t.watchers {
		task, ok := t.tasks[taskID]
		if !ok || task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskCancelled {
			for watchID, ch := range byTask {
				close(ch)
				delete(byTask, watchID)
			}
			delete(t.watchers, taskID)
		}
	}
}

// persist saves the task to the store, logging errors.
func (t *TaskTracker) persist(task *Task) {
	if t.store == nil {
		return
	}
	if err := t.store.Save(context.Background(), task); err != nil {
		slog.Default().Error("TaskTracker: persist failed",
			slog.String("task_id", task.ID),
			slog.Any("error", err))
	}
}

// restoreFromStore loads tasks from the store into the in-memory map.
func (t *TaskTracker) restoreFromStore(ctx context.Context) {
	if t.store == nil {
		return
	}
	tasks, err := t.store.List(ctx)
	if err != nil {
		slog.Default().Error("TaskTracker: restore from store failed", slog.Any("error", err))
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, task := range tasks {
		t.tasks[task.ID] = task
		if task.Revision > t.rev[task.ID] {
			t.rev[task.ID] = task.Revision
		}
	}
}

func (t *TaskTracker) mirror(task Task) {
	if t.runtime == nil {
		return
	}
	if err := t.runtime.UpsertTask(context.Background(), taskrt.TaskRecord{
		ID:              task.ID,
		AgentName:       task.AgentName,
		Goal:            task.Goal,
		Status:          taskrt.TaskStatus(task.Status),
		Contract:        task.Contract,
		ClaimedBy:       task.AgentName,
		WorkspaceID:     task.WorkspaceID,
		SessionID:       task.SessionID,
		ParentSessionID: task.ParentSessionID,
		JobID:           task.JobID,
		JobItemID:       task.JobItemID,
		Result:          task.Result,
		Error:           task.Error,
		CreatedAt:       task.CreatedAt,
		UpdatedAt:       task.UpdatedAt,
	}); err != nil {
		slog.Default().Error("task mirror: UpsertTask failed",
			slog.String("task_id", task.ID),
			slog.Any("error", err))
	}
	jobRuntime, ok := t.runtime.(taskrt.JobRuntime)
	if !ok {
		return
	}
	jobID := strings.TrimSpace(task.JobID)
	itemID := strings.TrimSpace(task.JobItemID)
	if jobID == "" {
		return
	}
	if err := jobRuntime.UpsertJob(context.Background(), taskrt.AgentJob{
		ID:        jobID,
		AgentName: task.AgentName,
		Goal:      task.Goal,
		Status:    jobStatusFromTask(task.Status),
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}); err != nil {
		slog.Default().Error("task mirror: UpsertJob failed",
			slog.String("task_id", task.ID),
			slog.String("job_id", jobID),
			slog.Any("error", err))
	}
	if itemID == "" {
		return
	}
	if err := jobRuntime.UpsertJobItem(context.Background(), taskrt.AgentJobItem{
		JobID:     jobID,
		ItemID:    itemID,
		Status:    jobStatusFromTask(task.Status),
		Executor:  task.AgentName,
		Result:    task.Result,
		Error:     task.Error,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}); err != nil {
		slog.Default().Error("task mirror: UpsertJobItem failed",
			slog.String("task_id", task.ID),
			slog.String("job_id", jobID),
			slog.String("item_id", itemID),
			slog.Any("error", err))
	}
}

func (t *TaskTracker) hydrate(ctx context.Context) {
	if t.runtime == nil {
		return
	}
	records, err := t.runtime.ListTasks(ctx, taskrt.TaskQuery{})
	if err != nil {
		return
	}
	var jobs map[string]taskrt.AgentJob
	if jobRuntime, ok := t.runtime.(taskrt.JobRuntime); ok {
		if listed, err := jobRuntime.ListJobs(ctx, taskrt.JobQuery{}); err == nil {
			jobs = make(map[string]taskrt.AgentJob, len(listed))
			for _, job := range listed {
				jobs[job.ID] = job
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, record := range records {
		task := &Task{
			ID:              record.ID,
			AgentName:       record.AgentName,
			Goal:            record.Goal,
			Status:          TaskStatus(record.Status),
			Contract:        record.Contract,
			Active:          false,
			SessionID:       record.SessionID,
			ParentSessionID: record.ParentSessionID,
			WorkspaceID:     record.WorkspaceID,
			JobID:           record.JobID,
			JobItemID:       record.JobItemID,
			Result:          record.Result,
			Error:           record.Error,
			CreatedAt:       record.CreatedAt,
			UpdatedAt:       record.UpdatedAt,
			Revision:        1,
		}
		if job, ok := jobs[task.JobID]; ok && job.Revision > 0 {
			task.Revision = job.Revision
		}
		t.tasks[task.ID] = task
		if task.Revision > t.rev[task.ID] {
			t.rev[task.ID] = task.Revision
		}
	}
}

func assignTaskJobIDs(task *Task, existing *Task, nextRev int64) {
	if task == nil {
		return
	}
	if strings.TrimSpace(task.JobID) == "" {
		switch {
		case existing != nil && existing.JobID != "" && existing.Status != TaskCompleted:
			task.JobID = existing.JobID
		case nextRev <= 1:
			task.JobID = task.ID
		default:
			task.JobID = task.ID + ":rev:" + strconv.FormatInt(nextRev, 10)
		}
	}
	if strings.TrimSpace(task.JobItemID) == "" {
		task.JobItemID = "turn-" + strconv.FormatInt(nextRev, 10)
	}
}

func jobStatusFromTask(status TaskStatus) taskrt.AgentJobStatus {
	switch status {
	case TaskRunning:
		return taskrt.JobRunning
	case TaskCompleted:
		return taskrt.JobCompleted
	case TaskFailed:
		return taskrt.JobFailed
	case TaskCancelled:
		return taskrt.JobCancelled
	default:
		return taskrt.JobPending
	}
}

func isActiveTask(task *Task) bool {
	return task != nil && task.Status == TaskRunning && task.Active
}

func isRecoverableTask(task *Task) bool {
	return task != nil && task.Status == TaskRunning && !task.Active
}
