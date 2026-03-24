package agent

import (
	"fmt"
	"sync"
)

// Registry 管理所有已注册的 Agent 配置。
type Registry struct {
	mu     sync.RWMutex
	agents map[string]AgentConfig
}

// NewRegistry 创建空的 Agent 注册表。
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]AgentConfig)}
}

// Register 注册一个 Agent 配置。如果同名 Agent 已存在则返回错误。
func (r *Registry) Register(cfg AgentConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agents[cfg.Name]; ok {
		return fmt.Errorf("agent %q already registered", cfg.Name)
	}
	r.agents[cfg.Name] = cfg
	return nil
}

// Get 按名称查找 Agent 配置。
func (r *Registry) Get(name string) (AgentConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.agents[name]
	return cfg, ok
}

// List 返回所有已注册 Agent 配置的快照。
func (r *Registry) List() []AgentConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentConfig, 0, len(r.agents))
	for _, cfg := range r.agents {
		out = append(out, cfg)
	}
	return out
}

// LoadDir 从目录中加载所有 Agent 配置文件并注册。
func (r *Registry) LoadDir(dir string) error {
	configs, err := LoadConfigsFromDir(dir)
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		if err := r.Register(cfg); err != nil {
			return err
		}
	}
	return nil
}
