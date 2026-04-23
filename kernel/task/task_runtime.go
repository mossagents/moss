package task

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/tool"
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
	ID              string       `json:"id"`
	AgentName       string       `json:"agent_name"`
	Goal            string       `json:"goal"`
	Status          TaskStatus   `json:"status"`
	DependsOn       []string     `json:"depends_on,omitempty"`
	ArtifactIDs     []string     `json:"artifact_ids,omitempty"`
	Contract        TaskContract `json:"contract,omitempty"`
	ClaimedBy       string       `json:"claimed_by,omitempty"`
	SwarmRunID      string       `json:"swarm_run_id,omitempty"`
	ThreadID        string       `json:"thread_id,omitempty"`
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	SessionID       string       `json:"session_id,omitempty"`
	ParentSessionID string       `json:"parent_session_id,omitempty"`
	JobID           string       `json:"job_id,omitempty"`
	JobItemID       string       `json:"job_item_id,omitempty"`
	Result          string       `json:"result,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreatedAt       time.Time    `json:"created_at,omitempty"`
	UpdatedAt       time.Time    `json:"updated_at,omitempty"`
}

// TaskBudget defines runtime ceilings owned by the supervisor.
type TaskBudget struct {
	MaxSteps   int `json:"max_steps,omitempty"`
	MaxTokens  int `json:"max_tokens,omitempty"`
	TimeoutSec int `json:"timeout_sec,omitempty"`
}

// TaskContract defines the explicit resource contract for a child task.
type TaskContract struct {
	TaskID          string             `json:"task_id,omitempty"`
	Goal            string             `json:"goal,omitempty"`
	InputContext    string             `json:"input_context,omitempty"`
	Budget          TaskBudget         `json:"budget,omitempty"`
	ApprovalCeiling tool.ApprovalClass `json:"approval_ceiling,omitempty"`
	WritableScopes  []string           `json:"writable_scopes,omitempty"`
	MemoryScope     string             `json:"memory_scope,omitempty"`
	AllowedEffects  []tool.Effect      `json:"allowed_effects,omitempty"`
	ReturnArtifacts []string           `json:"return_artifacts,omitempty"`
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
	AgentName       string     `json:"agent_name,omitempty"`
	Status          TaskStatus `json:"status,omitempty"`
	ClaimedBy       string     `json:"claimed_by,omitempty"`
	SwarmRunID      string     `json:"swarm_run_id,omitempty"`
	ThreadID        string     `json:"thread_id,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	ParentSessionID string     `json:"parent_session_id,omitempty"`
	JobID           string     `json:"job_id,omitempty"`
	Limit           int        `json:"limit,omitempty"`
}

// TaskRelationKind 表示任务与线程/作业的关系类型。
type TaskRelationKind string

const (
	TaskRelationDependency    TaskRelationKind = "depends_on"
	TaskRelationArtifact      TaskRelationKind = "artifact"
	TaskRelationSwarmRun      TaskRelationKind = "swarm_run"
	TaskRelationThread        TaskRelationKind = "thread"
	TaskRelationSession       TaskRelationKind = "session"
	TaskRelationParentSession TaskRelationKind = "parent_session"
	TaskRelationJob           TaskRelationKind = "job"
	TaskRelationJobItem       TaskRelationKind = "job_item"
)

// TaskHandle 是任务定位标识。
type TaskHandle struct {
	ID              string `json:"id"`
	SwarmRunID      string `json:"swarm_run_id,omitempty"`
	ThreadID        string `json:"thread_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	JobID           string `json:"job_id,omitempty"`
	JobItemID       string `json:"job_item_id,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
}

// TaskRelation 描述任务与其他对象的边。
type TaskRelation struct {
	TaskID            string           `json:"task_id"`
	Kind              TaskRelationKind `json:"kind"`
	RelatedTaskID     string           `json:"related_task_id,omitempty"`
	RelatedArtifactID string           `json:"related_artifact_id,omitempty"`
	RelatedRunID      string           `json:"related_run_id,omitempty"`
	RelatedThreadID   string           `json:"related_thread_id,omitempty"`
	RelatedSessionID  string           `json:"related_session_id,omitempty"`
	RelatedJobID      string           `json:"related_job_id,omitempty"`
	RelatedJobItemID  string           `json:"related_job_item_id,omitempty"`
}

// TaskSummary 是面向线程/子代理浏览的任务摘要。
type TaskSummary struct {
	Handle    TaskHandle     `json:"handle"`
	AgentName string         `json:"agent_name,omitempty"`
	Goal      string         `json:"goal,omitempty"`
	Status    TaskStatus     `json:"status"`
	ClaimedBy string         `json:"claimed_by,omitempty"`
	Result    string         `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	Relations []TaskRelation `json:"relations,omitempty"`
}

// TaskMessage is a persisted follow-up message queued for a task.
type TaskMessage struct {
	ID           string         `json:"id"`
	TaskID       string         `json:"task_id"`
	SwarmRunID   string         `json:"swarm_run_id,omitempty"`
	ThreadID     string         `json:"thread_id,omitempty"`
	FromThreadID string         `json:"from_thread_id,omitempty"`
	ToThreadID   string         `json:"to_thread_id,omitempty"`
	Kind         string         `json:"kind,omitempty"`
	Subject      string         `json:"subject,omitempty"`
	Content      string         `json:"content"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
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
//
// kernel/task 负责任务执行引擎（状态机、调度、依赖图、持久化），
// 是面向 Agent Loop 的底层运行时基础设施。
// 与 agent 包的区别：agent 包负责 Agent 委派和协作的上层逻辑（合约、深度控制），
// 使用 kernel/task 作为底层任务存储和调度引擎。
type TaskRuntime interface {
	UpsertTask(ctx context.Context, task TaskRecord) error
	GetTask(ctx context.Context, id string) (*TaskRecord, error)
	ListTasks(ctx context.Context, query TaskQuery) ([]TaskRecord, error)
	ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*TaskRecord, error)
}

// TaskMessageRuntime provides persistent queued follow-up messages for tasks.
type TaskMessageRuntime interface {
	EnqueueTaskMessage(ctx context.Context, message TaskMessage) (*TaskMessage, error)
	ListTaskMessages(ctx context.Context, taskID string, limit int) ([]TaskMessage, error)
	ConsumeTaskMessages(ctx context.Context, taskID string, limit int) ([]TaskMessage, error)
}

// TaskGraphRuntime 暴露线程/任务浏览所需的派生查询。
type TaskGraphRuntime interface {
	ListTaskSummaries(ctx context.Context, query TaskQuery) ([]TaskSummary, error)
	ListTaskRelations(ctx context.Context, taskID string) ([]TaskRelation, error)
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
	msgs  map[string][]TaskMessage
	seq   int64
}

func NewMemoryTaskRuntime() *MemoryTaskRuntime {
	return &MemoryTaskRuntime{
		tasks: make(map[string]TaskRecord),
		jobs:  make(map[string]AgentJob),
		items: make(map[string]map[string]AgentJobItem),
		msgs:  make(map[string][]TaskMessage),
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
	task.ArtifactIDs = append([]string(nil), task.ArtifactIDs...)
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
	cp.ArtifactIDs = append([]string(nil), task.ArtifactIDs...)
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
		if query.SwarmRunID != "" && t.SwarmRunID != query.SwarmRunID {
			continue
		}
		if query.ThreadID != "" && t.ThreadID != query.ThreadID {
			continue
		}
		if query.SessionID != "" && t.SessionID != query.SessionID {
			continue
		}
		if query.ParentSessionID != "" && t.ParentSessionID != query.ParentSessionID {
			continue
		}
		if query.JobID != "" && t.JobID != query.JobID {
			continue
		}
		cp := t
		cp.DependsOn = append([]string(nil), t.DependsOn...)
		cp.ArtifactIDs = append([]string(nil), t.ArtifactIDs...)
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
	cp.ArtifactIDs = append([]string(nil), best.ArtifactIDs...)
	return &cp, nil
}

func (r *MemoryTaskRuntime) EnqueueTaskMessage(_ context.Context, message TaskMessage) (*TaskMessage, error) {
	taskID := strings.TrimSpace(message.TaskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return nil, errors.New("content is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[taskID]; !ok {
		return nil, ErrTaskNotFound
	}
	r.seq++
	queued := TaskMessage{
		ID:           strings.TrimSpace(message.ID),
		TaskID:       taskID,
		SwarmRunID:   strings.TrimSpace(message.SwarmRunID),
		ThreadID:     strings.TrimSpace(message.ThreadID),
		FromThreadID: strings.TrimSpace(message.FromThreadID),
		ToThreadID:   strings.TrimSpace(message.ToThreadID),
		Kind:         strings.TrimSpace(message.Kind),
		Subject:      strings.TrimSpace(message.Subject),
		Content:      content,
		Metadata:     cloneAnyMap(message.Metadata),
		CreatedAt:    time.Now(),
	}
	if queued.ID == "" {
		queued.ID = "msg-" + taskID + "-" + strconv.FormatInt(r.seq, 10)
	}
	r.msgs[taskID] = append(r.msgs[taskID], queued)
	cp := cloneTaskMessage(queued)
	return &cp, nil
}

func (r *MemoryTaskRuntime) ListTaskMessages(_ context.Context, taskID string, limit int) ([]TaskMessage, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.tasks[taskID]; !ok {
		return nil, ErrTaskNotFound
	}
	original := r.msgs[taskID]
	out := make([]TaskMessage, 0, len(original))
	for _, item := range original {
		out = append(out, cloneTaskMessage(item))
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryTaskRuntime) ConsumeTaskMessages(_ context.Context, taskID string, limit int) ([]TaskMessage, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[taskID]; !ok {
		return nil, ErrTaskNotFound
	}
	original := r.msgs[taskID]
	if len(original) == 0 {
		return []TaskMessage{}, nil
	}
	count := len(original)
	if limit > 0 && limit < count {
		count = limit
	}
	consumed := make([]TaskMessage, 0, count)
	for i := 0; i < count; i++ {
		consumed = append(consumed, cloneTaskMessage(original[i]))
	}
	if count >= len(original) {
		delete(r.msgs, taskID)
	} else {
		rest := make([]TaskMessage, 0, len(original)-count)
		rest = append(rest, original[count:]...)
		r.msgs[taskID] = rest
	}
	return consumed, nil
}

func (r *MemoryTaskRuntime) ListTaskSummaries(ctx context.Context, query TaskQuery) ([]TaskSummary, error) {
	tasks, err := r.ListTasks(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, TaskSummaryFromRecord(task))
	}
	return out, nil
}

func (r *MemoryTaskRuntime) ListTaskRelations(ctx context.Context, taskID string) ([]TaskRelation, error) {
	task, err := r.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return TaskRelationsFromRecord(*task), nil
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

func TaskHandleFromRecord(task TaskRecord) TaskHandle {
	return TaskHandle{
		ID:              task.ID,
		SwarmRunID:      task.SwarmRunID,
		ThreadID:        task.ThreadID,
		SessionID:       task.SessionID,
		ParentSessionID: task.ParentSessionID,
		JobID:           task.JobID,
		JobItemID:       task.JobItemID,
		WorkspaceID:     task.WorkspaceID,
	}
}

func TaskRelationsFromRecord(task TaskRecord) []TaskRelation {
	out := make([]TaskRelation, 0, len(task.DependsOn)+len(task.ArtifactIDs)+6)
	for _, depID := range task.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		out = append(out, TaskRelation{
			TaskID:        task.ID,
			Kind:          TaskRelationDependency,
			RelatedTaskID: depID,
		})
	}
	for _, artifactID := range task.ArtifactIDs {
		artifactID = strings.TrimSpace(artifactID)
		if artifactID == "" {
			continue
		}
		out = append(out, TaskRelation{
			TaskID:            task.ID,
			Kind:              TaskRelationArtifact,
			RelatedArtifactID: artifactID,
		})
	}
	if runID := strings.TrimSpace(task.SwarmRunID); runID != "" {
		out = append(out, TaskRelation{
			TaskID:       task.ID,
			Kind:         TaskRelationSwarmRun,
			RelatedRunID: runID,
		})
	}
	if threadID := strings.TrimSpace(task.ThreadID); threadID != "" {
		out = append(out, TaskRelation{
			TaskID:          task.ID,
			Kind:            TaskRelationThread,
			RelatedRunID:    strings.TrimSpace(task.SwarmRunID),
			RelatedThreadID: threadID,
		})
	}
	if sessionID := strings.TrimSpace(task.SessionID); sessionID != "" {
		out = append(out, TaskRelation{
			TaskID:           task.ID,
			Kind:             TaskRelationSession,
			RelatedSessionID: sessionID,
		})
	}
	if parentID := strings.TrimSpace(task.ParentSessionID); parentID != "" {
		out = append(out, TaskRelation{
			TaskID:           task.ID,
			Kind:             TaskRelationParentSession,
			RelatedSessionID: parentID,
		})
	}
	if jobID := strings.TrimSpace(task.JobID); jobID != "" {
		out = append(out, TaskRelation{
			TaskID:       task.ID,
			Kind:         TaskRelationJob,
			RelatedJobID: jobID,
		})
	}
	if itemID := strings.TrimSpace(task.JobItemID); itemID != "" {
		out = append(out, TaskRelation{
			TaskID:           task.ID,
			Kind:             TaskRelationJobItem,
			RelatedJobID:     strings.TrimSpace(task.JobID),
			RelatedJobItemID: itemID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			left := strings.Join([]string{out[i].RelatedTaskID, out[i].RelatedArtifactID, out[i].RelatedRunID, out[i].RelatedThreadID, out[i].RelatedSessionID, out[i].RelatedJobID, out[i].RelatedJobItemID}, ":")
			right := strings.Join([]string{out[j].RelatedTaskID, out[j].RelatedArtifactID, out[j].RelatedRunID, out[j].RelatedThreadID, out[j].RelatedSessionID, out[j].RelatedJobID, out[j].RelatedJobItemID}, ":")
			return left < right
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func TaskSummaryFromRecord(task TaskRecord) TaskSummary {
	deps := append([]string(nil), task.DependsOn...)
	return TaskSummary{
		Handle:    TaskHandleFromRecord(task),
		AgentName: task.AgentName,
		Goal:      task.Goal,
		Status:    task.Status,
		ClaimedBy: task.ClaimedBy,
		Result:    task.Result,
		Error:     task.Error,
		DependsOn: deps,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
		Relations: TaskRelationsFromRecord(task),
	}
}

func cloneTaskMessage(in TaskMessage) TaskMessage {
	in.Metadata = cloneAnyMap(in.Metadata)
	return in
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
