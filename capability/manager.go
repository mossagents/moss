package capability

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// maxPromptTotalRunes caps the total rune length of all provider prompt additions.
const maxPromptTotalRunes = 16000

// promptTruncationMarker is appended when a prompt body is cut short.
const promptTruncationMarker = "\n... [capability prompt truncated]"

// Manager manages registered providers.
type Manager struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string
}

// NewManager creates a provider manager.
func NewManager() *Manager {
	return &Manager{providers: make(map[string]Provider)}
}

// Register registers and initializes a provider.
func (m *Manager) Register(ctx context.Context, p Provider, deps Deps) error {
	meta := p.Metadata()
	m.mu.Lock()
	if _, exists := m.providers[meta.Name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("capability %q already registered", meta.Name)
	}
	m.providers[meta.Name] = p
	m.order = append(m.order, meta.Name)
	m.mu.Unlock()

	if err := p.Init(ctx, deps); err != nil {
		m.mu.Lock()
		delete(m.providers, meta.Name)
		for i, name := range m.order {
			if name == meta.Name {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		return fmt.Errorf("init capability %q: %w", meta.Name, err)
	}

	return nil
}

// Unregister unregisters and shuts down a provider.
func (m *Manager) Unregister(ctx context.Context, name string) error {
	m.mu.Lock()
	p, exists := m.providers[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("capability %q not found", name)
	}
	delete(m.providers, name)
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	return p.Shutdown(ctx)
}

// List returns all registered provider metadata in registration order.
func (m *Manager) List() []Metadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Metadata, 0, len(m.order))
	for _, name := range m.order {
		if p, ok := m.providers[name]; ok {
			result = append(result, p.Metadata())
		}
	}
	return result
}

// Get looks up a provider by name.
func (m *Manager) Get(name string) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	return p, ok
}

// SystemPromptAdditions aggregates all provider prompt additions.
func (m *Manager) SystemPromptAdditions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	totalRunes := 0
	for _, name := range m.order {
		if totalRunes >= maxPromptTotalRunes {
			break
		}
		p, ok := m.providers[name]
		if !ok {
			continue
		}
		for _, prompt := range p.Metadata().Prompts {
			if totalRunes >= maxPromptTotalRunes {
				break
			}
			runes := []rune(prompt)
			remaining := maxPromptTotalRunes - totalRunes
			if len(runes) > remaining {
				prompt = string(runes[:remaining]) + promptTruncationMarker
				totalRunes = maxPromptTotalRunes
			} else {
				totalRunes += len(runes)
			}
			parts = append(parts, prompt)
		}
	}
	return strings.Join(parts, "\n\n")
}

// RegisterAll registers providers in dependency order.
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
func TopologicalSort(providers []Provider) ([]Provider, error) {
	index := make(map[string]Provider, len(providers))
	for _, p := range providers {
		index[p.Metadata().Name] = p
	}

	inDegree := make(map[string]int, len(providers))
	dependants := make(map[string][]string)

	for _, p := range providers {
		meta := p.Metadata()
		if _, ok := inDegree[meta.Name]; !ok {
			inDegree[meta.Name] = 0
		}
		for _, dep := range resolvedDepNames(meta) {
			if _, inSet := index[dep]; !inSet {
				continue
			}
			inDegree[meta.Name]++
			dependants[dep] = append(dependants[dep], meta.Name)
		}
	}

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
		return nil, fmt.Errorf("capability dependency cycle detected among providers")
	}
	return result, nil
}

// ValidateDeps checks that all dependencies of p are registered with satisfying versions.
func (m *Manager) ValidateDeps(p Provider) error {
	meta := p.Metadata()
	for _, req := range meta.Requires {
		dep, ok := m.Get(req.Name)
		if !ok {
			return fmt.Errorf("capability %q required by %q is not registered", req.Name, meta.Name)
		}
		depVersion := dep.Metadata().Version
		if !IsVersionInRange(depVersion, req.MinVersion, req.MaxVersion) {
			return fmt.Errorf("capability %q v%s does not satisfy %q requirement (min=%s max=%s)",
				req.Name, depVersion, meta.Name, req.MinVersion, req.MaxVersion)
		}
	}
	for _, name := range meta.DependsOn {
		if _, ok := m.Get(name); !ok {
			return fmt.Errorf("capability %q required by %q (depends_on) is not registered", name, meta.Name)
		}
	}
	return nil
}

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

// ShutdownAll shuts down all providers in reverse registration order.
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
		return fmt.Errorf("shutdown capabilities: %v", errs)
	}
	return nil
}
