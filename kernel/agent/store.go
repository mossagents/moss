package agent

import (
	"context"
	"sync"
)

// TaskFilter 定义 Task 查询过滤条件。
type TaskFilter struct {
	AgentName string     `json:"agent_name,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
}

// TaskStore 提供 Task 的持久化存储能力。
// 默认使用 InMemoryTaskStore（现有 TaskTracker 行为）。
// 多实例部署时可替换为 Redis/DB 实现。
type TaskStore interface {
	Save(ctx context.Context, task *Task) error
	Load(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, filter TaskFilter) ([]*Task, error)
}

// InMemoryTaskStore 是基于内存的 TaskStore 实现。
type InMemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewInMemoryTaskStore 创建内存 TaskStore。
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{tasks: make(map[string]*Task)}
}

func (s *InMemoryTaskStore) Save(_ context.Context, task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 存储副本防止外部修改
	cp := *task
	s.tasks[task.ID] = &cp
	return nil
}

func (s *InMemoryTaskStore) Load(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	cp := *task
	return &cp, nil
}

func (s *InMemoryTaskStore) List(_ context.Context, filter TaskFilter) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Task
	for _, t := range s.tasks {
		if filter.AgentName != "" && t.AgentName != filter.AgentName {
			continue
		}
		if filter.Status != "" && t.Status != filter.Status {
			continue
		}
		cp := *t
		result = append(result, &cp)
	}
	return result, nil
}
