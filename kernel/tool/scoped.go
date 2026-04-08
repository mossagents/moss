package tool

import (
	"fmt"
)

// ScopedRegistry 按工具名白名单过滤的只读 Registry 视图。
type ScopedRegistry struct {
	parent  Registry
	allowed map[string]struct{}
}

// Scoped 从现有 Registry 创建只包含指定工具的子集视图。
func Scoped(parent Registry, allowedTools []string) Registry {
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		allowed[name] = struct{}{}
	}
	return &ScopedRegistry{parent: parent, allowed: allowed}
}

func (s *ScopedRegistry) Register(spec ToolSpec, handler ToolHandler) error {
	return fmt.Errorf("scoped registry is read-only: cannot register tool %q", spec.Name)
}

func (s *ScopedRegistry) Unregister(name string) error {
	return fmt.Errorf("scoped registry is read-only: cannot unregister tool %q", name)
}

func (s *ScopedRegistry) Get(name string) (ToolSpec, ToolHandler, bool) {
	if _, ok := s.allowed[name]; !ok {
		return ToolSpec{}, nil, false
	}
	return s.parent.Get(name)
}

func (s *ScopedRegistry) List() []ToolSpec {
	var specs []ToolSpec
	for _, spec := range s.parent.List() {
		if _, ok := s.allowed[spec.Name]; ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (s *ScopedRegistry) ListByCapability(cap string) []ToolSpec {
	var specs []ToolSpec
	for _, spec := range s.parent.ListByCapability(cap) {
		if _, ok := s.allowed[spec.Name]; ok {
			specs = append(specs, spec)
		}
	}
	return specs
}
