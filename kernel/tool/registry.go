package tool

import (
	"fmt"
	"sync"
)

// Registry 管理工具的注册与查找。
type Registry interface {
	Register(spec ToolSpec, handler ToolHandler) error
	Unregister(name string) error
	Get(name string) (ToolSpec, ToolHandler, bool)
	List() []ToolSpec
	ListByCapability(cap string) []ToolSpec
}

type entry struct {
	spec    ToolSpec
	handler ToolHandler
}

type mapRegistry struct {
	mu      sync.RWMutex
	entries map[string]entry
	order   []string // insertion order for deterministic List()
}

// NewRegistry 创建基于 map 的默认 Registry 实现。
func NewRegistry() Registry {
	return &mapRegistry{entries: make(map[string]entry)}
}

func (r *mapRegistry) Register(spec ToolSpec, handler ToolHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[spec.Name]; ok {
		return fmt.Errorf("tool %q already registered", spec.Name)
	}
	r.entries[spec.Name] = entry{spec: spec, handler: handler}
	r.order = append(r.order, spec.Name)
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

func (r *mapRegistry) Get(name string) (ToolSpec, ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return ToolSpec{}, nil, false
	}
	return e.spec, e.handler, true
}

func (r *mapRegistry) List() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		if e, ok := r.entries[name]; ok {
			specs = append(specs, e.spec)
		}
	}
	return specs
}

func (r *mapRegistry) ListByCapability(cap string) []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []ToolSpec
	for _, name := range r.order {
		e, ok := r.entries[name]
		if !ok {
			continue
		}
		for _, c := range e.spec.Capabilities {
			if c == cap {
				specs = append(specs, e.spec)
				break
			}
		}
	}
	return specs
}
