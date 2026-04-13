package runtime

import (
	"context"
	"fmt"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

type config struct {
	builtin          bool
	mcpServers       bool
	skills           bool
	progressive      bool
	agents           bool
	trust            string
	sessionStore     session.SessionStore
	sessionStoreSet  bool
	planning         bool
	capabilityReport CapabilityReporter
}

type Option func(*config)

func defaultConfig() config {
	return config{
		builtin:          true,
		mcpServers:       true,
		skills:           true,
		progressive:      false,
		agents:           true,
		trust:            appconfig.TrustRestricted,
		planning:         true,
		capabilityReport: noopCapabilityReporter{},
	}
}

func WithBuiltinTools(enabled bool) Option { return func(c *config) { c.builtin = enabled } }
func WithMCPServers(enabled bool) Option   { return func(c *config) { c.mcpServers = enabled } }
func WithSkills(enabled bool) Option       { return func(c *config) { c.skills = enabled } }
func WithProgressiveSkills(enabled bool) Option {
	return func(c *config) { c.progressive = enabled }
}
func WithAgents(enabled bool) Option   { return func(c *config) { c.agents = enabled } }
func WithPlanning(enabled bool) Option { return func(c *config) { c.planning = enabled } }
func WithWorkspaceTrust(trust string) Option {
	return func(c *config) { c.trust = trust }
}
func WithSessionStore(store session.SessionStore) Option {
	return func(c *config) {
		c.sessionStore = store
		c.sessionStoreSet = true
	}
}

func WithCapabilityReporter(r CapabilityReporter) Option {
	return func(c *config) {
		if r == nil {
			c.capabilityReport = noopCapabilityReporter{}
			return
		}
		c.capabilityReport = r
	}
}

type CapabilityReporter interface {
	Report(ctx context.Context, capability string, critical bool, state string, err error)
}

type noopCapabilityReporter struct{}

func (noopCapabilityReporter) Report(context.Context, string, bool, string, error) {}

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

func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	cfg, err := resolve(opts...)
	if err != nil {
		return err
	}
	cfg.capabilityReport = NewCapabilityReporter(CapabilityStatusPath(), cfg.capabilityReport)
	SetExecutionPolicy(k, ResolveExecutionPolicyForKernel(k, cfg.trust, "confirm"))
	return newRuntimeLifecycleManager().Run(ctx, k, workspaceDir, cfg)
}
