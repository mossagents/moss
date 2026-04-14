package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TaskFilter 定义 Task 查询过滤条件。
type TaskFilter struct {
	AgentName string     `json:"agent_name,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
}

// TaskStore provides pluggable persistence for TaskTracker.
type TaskStore interface {
	// Save persists a task.
	Save(ctx context.Context, task *Task) error
	// Load retrieves a task by ID.
	Load(ctx context.Context, id string) (*Task, error)
	// List returns all tasks.
	List(ctx context.Context) ([]*Task, error)
	// Delete removes a task by ID.
	Delete(ctx context.Context, id string) error
}

// MemoryTaskStore is the default in-memory TaskStore implementation.
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewMemoryTaskStore creates an in-memory TaskStore.
func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{tasks: make(map[string]*Task)}
}

func (s *MemoryTaskStore) Save(_ context.Context, task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *task
	s.tasks[task.ID] = &cp
	return nil
}

func (s *MemoryTaskStore) Load(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	cp := *task
	return &cp, nil
}

func (s *MemoryTaskStore) List(_ context.Context) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cp := *t
		result = append(result, &cp)
	}
	return result, nil
}

func (s *MemoryTaskStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

// FileTaskStore persists tasks as a JSON array in a single file.
type FileTaskStore struct {
	mu   sync.RWMutex
	path string
}

// NewFileTaskStore creates a FileTaskStore that reads/writes tasks to the
// given file path. The parent directory must already exist.
func NewFileTaskStore(path string) *FileTaskStore {
	return &FileTaskStore{path: path}
}

func (s *FileTaskStore) Save(ctx context.Context, task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.readLocked()
	if err != nil {
		return err
	}
	cp := *task
	found := false
	for i, t := range tasks {
		if t.ID == task.ID {
			tasks[i] = &cp
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, &cp)
	}
	return s.writeLocked(tasks)
}

func (s *FileTaskStore) Load(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if t.ID == id {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *FileTaskStore) List(_ context.Context) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	result := make([]*Task, len(tasks))
	for i, t := range tasks {
		cp := *t
		result[i] = &cp
	}
	return result, nil
}

func (s *FileTaskStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.readLocked()
	if err != nil {
		return err
	}
	filtered := make([]*Task, 0, len(tasks))
	for _, t := range tasks {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	return s.writeLocked(filtered)
}

// readLocked reads tasks from the file. Caller must hold at least s.mu.RLock.
func (s *FileTaskStore) readLocked() ([]*Task, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read task store file: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal task store file: %w", err)
	}
	return tasks, nil
}

// writeLocked writes tasks to the file atomically. Caller must hold s.mu.Lock.
func (s *FileTaskStore) writeLocked(tasks []*Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task store: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".task-store-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
