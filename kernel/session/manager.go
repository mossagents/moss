package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/ids"
	"github.com/mossagents/moss/kernel/model"
)

// Manager 管理 Session 的生命周期。
type Manager interface {
	Create(ctx context.Context, cfg SessionConfig) (*Session, error)
	Get(id string) (*Session, bool)
	List() []*Session
	Cancel(id string) error
	Notify(id string, msg model.Message) error // 跨 Session 注入消息
}

// CancelHookAware 是可选扩展契约：实现后可接收 Session.Cancel 的回调。
// Kernel 装配层会通过该回调把 Session 取消贯通到运行中的 run cancellation。
type CancelHookAware interface {
	SetCancelHook(func(id string))
}

type memoryManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	onCancel func(id string)
}

type cancelHookManager struct {
	base Manager
	mu   sync.RWMutex

	onCancel func(id string)
}

// NewManager 创建基于内存的默认 SessionManager 实现。
func NewManager() Manager {
	return &memoryManager{
		sessions: make(map[string]*Session),
	}
}

// WithCancelHook 将取消回调安装到给定 Manager，并返回可用的 Manager。
// 若 m 实现了 CancelHookAware，会直接设置回调；否则返回一个包装器来提供该能力。
func WithCancelHook(m Manager, onCancel func(id string)) Manager {
	if m == nil {
		return nil
	}
	if aware, ok := m.(CancelHookAware); ok {
		aware.SetCancelHook(onCancel)
		return m
	}
	return &cancelHookManager{
		base:     m,
		onCancel: onCancel,
	}
}

func (m *memoryManager) SetCancelHook(onCancel func(id string)) {
	m.mu.Lock()
	m.onCancel = onCancel
	m.mu.Unlock()
}

func (m *memoryManager) Create(_ context.Context, cfg SessionConfig) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := ids.New()

	s := &Session{
		ID:        id,
		Status:    StatusCreated,
		Config:    cfg,
		Messages:  make([]model.Message, 0),
		State:     ScopedState{},
		Budget:    Budget{MaxTokens: cfg.MaxTokens, MaxSteps: cfg.MaxSteps},
		CreatedAt: time.Now(),
	}
	m.sessions[id] = s
	return s, nil
}

func (m *memoryManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *memoryManager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
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

func (m *memoryManager) Notify(id string, msg model.Message) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	s.AppendMessage(msg)
	return nil
}

func (m *cancelHookManager) SetCancelHook(onCancel func(id string)) {
	m.mu.Lock()
	m.onCancel = onCancel
	m.mu.Unlock()
}

func (m *cancelHookManager) Create(ctx context.Context, cfg SessionConfig) (*Session, error) {
	return m.base.Create(ctx, cfg)
}

func (m *cancelHookManager) Get(id string) (*Session, bool) {
	return m.base.Get(id)
}

func (m *cancelHookManager) List() []*Session {
	return m.base.List()
}

func (m *cancelHookManager) Cancel(id string) error {
	if err := m.base.Cancel(id); err != nil {
		return err
	}
	m.mu.RLock()
	onCancel := m.onCancel
	m.mu.RUnlock()
	if onCancel != nil {
		onCancel(id)
	}
	return nil
}

func (m *cancelHookManager) Notify(id string, msg model.Message) error {
	return m.base.Notify(id, msg)
}
