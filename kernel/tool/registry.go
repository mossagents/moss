package tool

import (
	"fmt"
	"sync"
)

// Registry 管理工具的注册与查找。
type Registry interface {
	Register(t Tool) error
	Unregister(name string) error
	Get(name string) (Tool, bool)
	List() []Tool
	ListByCapability(cap string) []Tool
}

type mapRegistry struct {
	mu      sync.RWMutex
	entries map[string]Tool
	order   []string // insertion order for deterministic List()
}

// NewRegistry 创建基于 map 的默认 Registry 实现。
func NewRegistry() Registry {
	return &mapRegistry{entries: make(map[string]Tool)}
}

func (r *mapRegistry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, ok := r.entries[name]; ok {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.entries[name] = t
	r.order = append(r.order, name)
	return nil
}

func (r *mapRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[name]; !ok {
		return fmt.Errorf("tool %q not found", name)
	}
	delete(r.entries, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return nil
}

func (r *mapRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.entries[name]
	return t, ok
}

func (r *mapRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.entries[name]; ok {
			tools = append(tools, t)
		}
	}
	return tools
}

func (r *mapRegistry) ListByCapability(cap string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var tools []Tool
	for _, name := range r.order {
		t, ok := r.entries[name]
		if !ok {
			continue
		}
		for _, c := range t.Spec().Capabilities {
			if c == cap {
				tools = append(tools, t)
				break
			}
		}
	}
	return tools
}
