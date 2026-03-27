package runtime

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/extensions/agentsx"
	"github.com/mossagents/moss/extensions/defaults"
	"github.com/mossagents/moss/extensions/skillsx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/skill"
)

type config struct {
	builtin        bool
	mcpServers     bool
	skills         bool
	progressive    bool
	agents         bool
	sessionStore   session.SessionStore
	sessionStoreSet bool
	planning       bool
}

// Option controls runtime setup behavior.
type Option func(*config)

func defaultConfig() config {
	return config{
		builtin:    true,
		mcpServers: true,
		skills:     true,
		progressive:false,
		agents:     true,
		planning:   true,
	}
}

func WithBuiltinTools(enabled bool) Option { return func(c *config) { c.builtin = enabled } }
func WithMCPServers(enabled bool) Option   { return func(c *config) { c.mcpServers = enabled } }
func WithSkills(enabled bool) Option       { return func(c *config) { c.skills = enabled } }
func WithProgressiveSkills(enabled bool) Option {
	return func(c *config) { c.progressive = enabled }
}
func WithAgents(enabled bool) Option { return func(c *config) { c.agents = enabled } }
func WithPlanning(enabled bool) Option { return func(c *config) { c.planning = enabled } }
func WithSessionStore(store session.SessionStore) Option {
	return func(c *config) {
		c.sessionStore = store
		c.sessionStoreSet = true
	}
}

func resolve(opts ...Option) (config, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !cfg.skills && cfg.progressive {
		return cfg, fmt.Errorf("invalid runtime options: progressive skills require skills to be enabled")
	}
	if cfg.sessionStoreSet && cfg.sessionStore == nil {
		return cfg, fmt.Errorf("invalid runtime options: session store cannot be nil")
	}
	return cfg, nil
}

// Setup composes default runtime features onto an existing kernel.
func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	cfg, err := resolve(opts...)
	if err != nil {
		return err
	}
	var mapped []defaults.Option
	if !cfg.builtin {
		mapped = append(mapped, defaults.WithoutBuiltin())
	}
	if !cfg.mcpServers {
		mapped = append(mapped, defaults.WithoutMCPServers())
	}
	if !cfg.skills {
		mapped = append(mapped, defaults.WithoutSkills())
	}
	if cfg.progressive {
		mapped = append(mapped, defaults.WithProgressiveSkills())
	}
	if err := defaults.Setup(ctx, k, workspaceDir, mapped...); err != nil {
		return err
	}
	return nil
}

func SkillsManager(k *kernel.Kernel) *skill.Manager {
	return skillsx.Manager(k)
}

func SkillManifests(k *kernel.Kernel) []skill.Manifest {
	return skillsx.Manifests(k)
}

func SetSkillManifests(k *kernel.Kernel, manifests []skill.Manifest) {
	skillsx.SetManifests(k, manifests)
}

func EnableProgressiveSkills(k *kernel.Kernel) {
	skillsx.EnableProgressive(k)
}

func RegisterProgressiveSkillTools(k *kernel.Kernel) error {
	return skillsx.RegisterProgressiveTools(k)
}

func AgentRegistry(k *kernel.Kernel) *agent.Registry {
	return agentsx.Registry(k)
}
