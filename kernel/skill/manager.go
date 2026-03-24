package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Manager 管理所有已加载的 skills。
type Manager struct {
	mu     sync.RWMutex
	skills map[string]Provider
	order  []string // 按加载顺序保存 skill 名称
}

// NewManager 创建 SkillManager。
func NewManager() *Manager {
	return &Manager{skills: make(map[string]Provider)}
}

// Register 注册并初始化一个 skill。
func (m *Manager) Register(ctx context.Context, s Provider, deps Deps) error {
	meta := s.Metadata()
	m.mu.Lock()
	if _, exists := m.skills[meta.Name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("skill %q already registered", meta.Name)
	}
	m.skills[meta.Name] = s
	m.order = append(m.order, meta.Name)
	m.mu.Unlock()

	if err := s.Init(ctx, deps); err != nil {
		m.mu.Lock()
		delete(m.skills, meta.Name)
		// 从 order 中移除
		for i, name := range m.order {
			if name == meta.Name {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		return fmt.Errorf("init skill %q: %w", meta.Name, err)
	}

	return nil
}

// Unregister 注销并关闭一个 skill。
func (m *Manager) Unregister(ctx context.Context, name string) error {
	m.mu.Lock()
	s, exists := m.skills[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("skill %q not found", name)
	}
	delete(m.skills, name)
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	return s.Shutdown(ctx)
}

// List 返回所有已加载 skill 的元信息（按加载顺序）。
func (m *Manager) List() []Metadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Metadata, 0, len(m.order))
	for _, name := range m.order {
		if s, ok := m.skills[name]; ok {
			result = append(result, s.Metadata())
		}
	}
	return result
}

// Get 按名称查找 skill。
func (m *Manager) Get(name string) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// SystemPromptAdditions 汇总所有 skill 提供的 system prompt 片段。
func (m *Manager) SystemPromptAdditions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	for _, name := range m.order {
		if s, ok := m.skills[name]; ok {
			for _, p := range s.Metadata().Prompts {
				parts = append(parts, p)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// ShutdownAll 关闭所有 skill（逆序）。
func (m *Manager) ShutdownAll(ctx context.Context) error {
	m.mu.Lock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	m.mu.Unlock()

	var errs []error
	for i := len(names) - 1; i >= 0; i-- {
		if err := m.Unregister(ctx, names[i]); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown skills: %v", errs)
	}
	return nil
}
