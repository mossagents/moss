package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// maxSkillPromptTotalRunes caps the total rune length of all skill prompt
// additions combined. Prevents large or numerous skills from overflowing the
// model's system-prompt budget.
const maxSkillPromptTotalRunes = 16000

// skillPromptTruncationMarker is appended when a prompt body is cut short.
const skillPromptTruncationMarker = "\n... [skill prompt truncated]"

// Manager 管理所有已加载的 providers。
// builtin tools、prompt skills、MCP providers 都通过它统一完成注册、生命周期和提示词聚合。
type Manager struct {
	mu     sync.RWMutex
	skills map[string]Provider
	order  []string // 按加载顺序保存 skill 名称
}

// NewManager 创建 provider manager。
func NewManager() *Manager {
	return &Manager{skills: make(map[string]Provider)}
}

// Register 注册并初始化一个 provider。
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

// Unregister 注销并关闭一个 provider。
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

// List 返回所有已加载 provider 的元信息（按加载顺序）。
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

// Get 按名称查找 provider。
func (m *Manager) Get(name string) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// SystemPromptAdditions 汇总所有 provider 提供的 system prompt 片段。
// 总长度以 rune 为单位上限为 maxSkillPromptTotalRunes，超出部分截断。
func (m *Manager) SystemPromptAdditions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	totalRunes := 0
	for _, name := range m.order {
		if totalRunes >= maxSkillPromptTotalRunes {
			break
		}
		s, ok := m.skills[name]
		if !ok {
			continue
		}
		for _, p := range s.Metadata().Prompts {
			if totalRunes >= maxSkillPromptTotalRunes {
				break
			}
			runes := []rune(p)
			remaining := maxSkillPromptTotalRunes - totalRunes
			if len(runes) > remaining {
				p = string(runes[:remaining]) + skillPromptTruncationMarker
				totalRunes = maxSkillPromptTotalRunes
			} else {
				totalRunes += len(runes)
			}
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// RegisterAll 按拓扑顺序注册多个 provider，检测循环依赖并验证版本约束。
func (m *Manager) RegisterAll(ctx context.Context, providers []Provider, deps Deps) error {
	sorted, err := TopologicalSort(providers)
	if err != nil {
		return err
	}
	for _, p := range sorted {
		if err := m.Register(ctx, p, deps); err != nil {
			return err
		}
		if err := m.ValidateDeps(p); err != nil {
			_ = m.Unregister(ctx, p.Metadata().Name)
			return err
		}
	}
	return nil
}

// TopologicalSort returns providers in dependency-first order.
// Returns an error if a cycle is detected.
func TopologicalSort(providers []Provider) ([]Provider, error) {
	index := make(map[string]Provider, len(providers))
	for _, p := range providers {
		index[p.Metadata().Name] = p
	}

	// Build adjacency list: deps → dependant (for Kahn's algorithm)
	inDegree := make(map[string]int, len(providers))
	dependants := make(map[string][]string) // name → slice of names that depend on it

	for _, p := range providers {
		meta := p.Metadata()
		if _, ok := inDegree[meta.Name]; !ok {
			inDegree[meta.Name] = 0
		}
		// Collect all dependency names (Requires takes priority, then DependsOn)
		depNames := resolvedDepNames(meta)
		for _, dep := range depNames {
			if _, inSet := index[dep]; !inSet {
				continue // external dep, skip for sort purposes
			}
			inDegree[meta.Name]++
			dependants[dep] = append(dependants[dep], meta.Name)
		}
	}

	// Kahn's algorithm
	queue := make([]string, 0, len(providers))
	for _, p := range providers {
		if inDegree[p.Metadata().Name] == 0 {
			queue = append(queue, p.Metadata().Name)
		}
	}

	result := make([]Provider, 0, len(providers))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, index[name])
		for _, dep := range dependants[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(providers) {
		return nil, fmt.Errorf("skill dependency cycle detected among providers")
	}
	return result, nil
}

// ValidateDeps checks that all dependencies of p are registered with satisfying versions.
func (m *Manager) ValidateDeps(p Provider) error {
	meta := p.Metadata()

	// Check Requires (version-constrained)
	for _, req := range meta.Requires {
		dep, ok := m.Get(req.Name)
		if !ok {
			return fmt.Errorf("skill %q required by %q is not registered", req.Name, meta.Name)
		}
		depVersion := dep.Metadata().Version
		if !IsVersionInRange(depVersion, req.MinVersion, req.MaxVersion) {
			return fmt.Errorf("skill %q v%s does not satisfy %q requirement (min=%s max=%s)",
				req.Name, depVersion, meta.Name, req.MinVersion, req.MaxVersion)
		}
	}

	// Check legacy DependsOn (name-only)
	for _, name := range meta.DependsOn {
		if _, ok := m.Get(name); !ok {
			return fmt.Errorf("skill %q required by %q (depends_on) is not registered", name, meta.Name)
		}
	}
	return nil
}

// resolvedDepNames returns all dependency names for a skill, deduped.
func resolvedDepNames(meta Metadata) []string {
	seen := make(map[string]bool)
	var names []string
	for _, req := range meta.Requires {
		if !seen[req.Name] {
			seen[req.Name] = true
			names = append(names, req.Name)
		}
	}
	for _, name := range meta.DependsOn {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ShutdownAll 关闭所有 provider（逆序）。
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
