package policystate

import (
	"sync"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks/builtins"
)

const serviceKey kernel.ServiceKey = "tool-policy.state"

type State struct {
	mu sync.RWMutex

	payload  map[string]any
	summary  map[string]any
	compiled []builtins.PolicyRule

	toolHookInstalled    bool
	sessionHookInstalled bool
}

func Ensure(k *kernel.Kernel) *State {
	if k == nil {
		return nil
	}
	actual, _ := k.Services().LoadOrStore(serviceKey, &State{})
	return actual.(*State)
}

func Lookup(k *kernel.Kernel) (*State, bool) {
	if k == nil {
		return nil, false
	}
	actual, ok := k.Services().Load(serviceKey)
	if !ok || actual == nil {
		return nil, false
	}
	st, ok := actual.(*State)
	return st, ok
}

func (s *State) Set(payload, summary map[string]any, compiled []builtins.PolicyRule) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = cloneMap(payload)
	s.summary = cloneMap(summary)
	s.compiled = append([]builtins.PolicyRule(nil), compiled...)
}

func (s *State) Payload() map[string]any {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMap(s.payload)
}

func (s *State) Summary() map[string]any {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMap(s.summary)
}

func (s *State) CompiledRules() []builtins.PolicyRule {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]builtins.PolicyRule(nil), s.compiled...)
}

func (s *State) MarkToolHookInstalled() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	already := s.toolHookInstalled
	s.toolHookInstalled = true
	return already
}

func (s *State) MarkSessionHookInstalled() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	already := s.sessionHookInstalled
	s.sessionHookInstalled = true
	return already
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
