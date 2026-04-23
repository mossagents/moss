package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type fileTaskRuntimeState struct {
	Tasks map[string]TaskRecord              `json:"tasks"`
	Jobs  map[string]AgentJob                `json:"jobs,omitempty"`
	Items map[string]map[string]AgentJobItem `json:"items,omitempty"`
	Msgs  map[string][]TaskMessage           `json:"messages,omitempty"`
	Seq   int64                              `json:"sequence,omitempty"`
}

// FileTaskRuntime 是基于文件系统的 TaskRuntime 实现。
// 运行时状态保存为单个 JSON 文件，适合作为本地恢复模型的默认实现。
type FileTaskRuntime struct {
	mu    sync.Mutex
	path  string
	tasks map[string]TaskRecord
	jobs  map[string]AgentJob
	items map[string]map[string]AgentJobItem
	msgs  map[string][]TaskMessage
	seq   int64
}

func NewFileTaskRuntime(dir string) (*FileTaskRuntime, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create task runtime dir: %w", err)
	}
	rt := &FileTaskRuntime{
		path:  filepath.Join(dir, "tasks.json"),
		tasks: make(map[string]TaskRecord),
		jobs:  make(map[string]AgentJob),
		items: make(map[string]map[string]AgentJobItem),
		msgs:  make(map[string][]TaskMessage),
	}
	if err := rt.load(); err != nil {
		return nil, err
	}
	return rt, nil
}

func (r *FileTaskRuntime) UpsertTask(_ context.Context, task TaskRecord) error {
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
	return r.persist()
}

func (r *FileTaskRuntime) GetTask(_ context.Context, id string) (*TaskRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	cp := task
	cp.DependsOn = append([]string(nil), task.DependsOn...)
	cp.ArtifactIDs = append([]string(nil), task.ArtifactIDs...)
	return &cp, nil
}

func (r *FileTaskRuntime) ListTasks(_ context.Context, query TaskQuery) ([]TaskRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *FileTaskRuntime) ClaimNextReady(_ context.Context, claimer string, preferredAgent string) (*TaskRecord, error) {
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
	if err := r.persist(); err != nil {
		return nil, err
	}
	cp := *best
	cp.DependsOn = append([]string(nil), best.DependsOn...)
	cp.ArtifactIDs = append([]string(nil), best.ArtifactIDs...)
	return &cp, nil
}

func (r *FileTaskRuntime) ListTaskSummaries(ctx context.Context, query TaskQuery) ([]TaskSummary, error) {
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

func (r *FileTaskRuntime) ListTaskRelations(ctx context.Context, taskID string) ([]TaskRelation, error) {
	task, err := r.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return TaskRelationsFromRecord(*task), nil
}

func (r *FileTaskRuntime) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read task runtime state: %w", err)
	}
	var state fileTaskRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal task runtime state: %w", err)
	}
	if state.Tasks == nil {
		state.Tasks = make(map[string]TaskRecord)
	}
	if state.Jobs == nil {
		state.Jobs = make(map[string]AgentJob)
	}
	if state.Items == nil {
		state.Items = make(map[string]map[string]AgentJobItem)
	}
	if state.Msgs == nil {
		state.Msgs = make(map[string][]TaskMessage)
	}
	r.tasks = state.Tasks
	r.jobs = state.Jobs
	r.items = state.Items
	r.msgs = state.Msgs
	r.seq = state.Seq
	return nil
}

func (r *FileTaskRuntime) persist() error {
	state := fileTaskRuntimeState{
		Tasks: make(map[string]TaskRecord, len(r.tasks)),
		Jobs:  make(map[string]AgentJob, len(r.jobs)),
		Items: make(map[string]map[string]AgentJobItem, len(r.items)),
		Msgs:  make(map[string][]TaskMessage, len(r.msgs)),
		Seq:   r.seq,
	}
	for id, task := range r.tasks {
		cp := task
		cp.DependsOn = append([]string(nil), task.DependsOn...)
		cp.ArtifactIDs = append([]string(nil), task.ArtifactIDs...)
		state.Tasks[id] = cp
	}
	for id, job := range r.jobs {
		state.Jobs[id] = job
	}
	for jobID, byItem := range r.items {
		cpMap := make(map[string]AgentJobItem, len(byItem))
		for itemID, item := range byItem {
			cpMap[itemID] = item
		}
		state.Items[jobID] = cpMap
	}
	for taskID, list := range r.msgs {
		cp := make([]TaskMessage, 0, len(list))
		for _, item := range list {
			cp = append(cp, item)
		}
		state.Msgs[taskID] = cp
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task runtime state: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write task runtime tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("replace task runtime state: %w", err)
	}
	return nil
}

func (r *FileTaskRuntime) UpsertJob(_ context.Context, job AgentJob) error {
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
	return r.persist()
}

func (r *FileTaskRuntime) GetJob(_ context.Context, id string) (*AgentJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	cp := job
	return &cp, nil
}

func (r *FileTaskRuntime) ListJobs(_ context.Context, query JobQuery) ([]AgentJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *FileTaskRuntime) UpsertJobItem(_ context.Context, item AgentJobItem) error {
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
	return r.persist()
}

func (r *FileTaskRuntime) MarkJobItemRunning(_ context.Context, jobID, itemID, executor string) (*AgentJobItem, error) {
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
	if err := r.persist(); err != nil {
		return nil, err
	}
	cp := item
	return &cp, nil
}

func (r *FileTaskRuntime) ReportJobItemResult(_ context.Context, jobID, itemID, executor string, status AgentJobStatus, result string, errMsg string) (*AgentJobItem, error) {
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
	if err := r.persist(); err != nil {
		return nil, err
	}
	cp := item
	return &cp, nil
}

func (r *FileTaskRuntime) ListJobItems(_ context.Context, query JobItemQuery) ([]AgentJobItem, error) {
	if query.JobID == "" {
		return nil, errors.New("job_id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *FileTaskRuntime) EnqueueTaskMessage(_ context.Context, message TaskMessage) (*TaskMessage, error) {
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
		queued.ID = fmt.Sprintf("msg-%s-%d", taskID, r.seq)
	}
	r.msgs[taskID] = append(r.msgs[taskID], queued)
	if err := r.persist(); err != nil {
		return nil, err
	}
	cp := cloneTaskMessage(queued)
	return &cp, nil
}

func (r *FileTaskRuntime) ListTaskMessages(_ context.Context, taskID string, limit int) ([]TaskMessage, error) {
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
	out := make([]TaskMessage, 0, len(original))
	for _, item := range original {
		out = append(out, cloneTaskMessage(item))
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *FileTaskRuntime) ConsumeTaskMessages(_ context.Context, taskID string, limit int) ([]TaskMessage, error) {
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
	if err := r.persist(); err != nil {
		return nil, err
	}
	return consumed, nil
}
