package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// Manager 管理 Session 的生命周期。
type Manager interface {
	Create(ctx context.Context, cfg SessionConfig) (*Session, error)
	Get(id string) (*Session, bool)
	List() []*Session
	Cancel(id string) error
	Notify(id string, msg port.Message) error // 跨 Session 注入消息
}

type memoryManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	nextID   int
	onCancel func(id string)
}

// NewManager 创建基于内存的默认 SessionManager 实现。
func NewManager() Manager {
	return &memoryManager{
		sessions: make(map[string]*Session),
	}
}

// AttachCancelHook 为支持内存实现的 SessionManager 安装取消回调。
// 非内存实现将被忽略，以保持 Manager 接口最小稳定。
func AttachCancelHook(m Manager, onCancel func(id string)) {
	if mm, ok := m.(*memoryManager); ok {
		mm.mu.Lock()
		mm.onCancel = onCancel
		mm.mu.Unlock()
	}
}

func (m *memoryManager) Create(_ context.Context, cfg SessionConfig) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	id := fmt.Sprintf("sess_%d", m.nextID)

	s := &Session{
		ID:        id,
		Status:    StatusCreated,
		Config:    cfg,
		Messages:  make([]port.Message, 0),
		State:     make(map[string]any),
		Budget:    Budget{MaxTokens: cfg.MaxTokens, MaxSteps: cfg.MaxSteps},
		CreatedAt: time.Now(),
	}
	m.sessions[id] = s
	return s, nil
}

func (m *memoryManager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *memoryManager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

func (m *memoryManager) Cancel(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	s.Status = StatusCancelled
	s.EndedAt = time.Now()
	onCancel := m.onCancel
	m.mu.Unlock()
	if onCancel != nil {
		onCancel(id)
	}
	return nil
}

func (m *memoryManager) Notify(id string, msg port.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	s.Messages = append(s.Messages, msg)
	return nil
}
