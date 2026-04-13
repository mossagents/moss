package kernel

import "sync"

// ServiceKey identifies a kernel-scoped shared service or state slot.
type ServiceKey string

// ServiceRegistry manages kernel-scoped shared state with load-or-store semantics.
type ServiceRegistry struct {
	mu     sync.RWMutex
	values map[ServiceKey]any
}

func newServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{values: make(map[ServiceKey]any)}
}

func (r *ServiceRegistry) Load(key ServiceKey) (any, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[key]
	return value, ok
}

func (r *ServiceRegistry) Store(key ServiceKey, value any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
}

func (r *ServiceRegistry) LoadOrStore(key ServiceKey, value any) (actual any, loaded bool) {
	if r == nil {
		return value, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if actual, loaded = r.values[key]; loaded {
		return actual, true
	}
	r.values[key] = value
	return value, false
}
