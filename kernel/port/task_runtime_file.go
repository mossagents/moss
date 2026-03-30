package port

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type fileTaskRuntimeState struct {
	Tasks map[string]TaskRecord `json:"tasks"`
}

// FileTaskRuntime 是基于文件系统的 TaskRuntime 实现。
// 运行时状态保存为单个 JSON 文件，适合作为本地恢复模型的默认实现。
type FileTaskRuntime struct {
	mu    sync.Mutex
	path  string
	tasks map[string]TaskRecord
}

func NewFileTaskRuntime(dir string) (*FileTaskRuntime, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create task runtime dir: %w", err)
	}
	rt := &FileTaskRuntime{
		path:  filepath.Join(dir, "tasks.json"),
		tasks: make(map[string]TaskRecord),
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
	return &cp, nil
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
	r.tasks = state.Tasks
	return nil
}

func (r *FileTaskRuntime) persist() error {
	state := fileTaskRuntimeState{
		Tasks: make(map[string]TaskRecord, len(r.tasks)),
	}
	for id, task := range r.tasks {
		cp := task
		cp.DependsOn = append([]string(nil), task.DependsOn...)
		state.Tasks[id] = cp
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
