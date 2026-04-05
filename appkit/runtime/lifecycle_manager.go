package runtime

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel"
)

type runtimeCapability interface {
	Name() string
	Critical() bool
	Enabled(config) bool
	Register(context.Context, *kernel.Kernel, string, config) error
	Validate(context.Context, *kernel.Kernel, string, config) error
	Activate(context.Context, *kernel.Kernel, string, config) error
}

type runtimeLifecycleManager struct {
	capabilities []runtimeCapability
}

func newRuntimeLifecycleManager() runtimeLifecycleManager {
	return runtimeLifecycleManager{
		capabilities: []runtimeCapability{
			builtinToolsCapability{},
			mcpCapability{},
			skillsCapability{},
			agentsCapability{},
		},
	}
}

func (m runtimeLifecycleManager) Run(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			cfg.capabilityReport.Report(ctx, cap.Name(), cap.Critical(), "disabled", nil)
			continue
		}
		if err := cap.Register(ctx, k, workspaceDir, cfg); err != nil {
			cfg.capabilityReport.Report(ctx, cap.Name(), cap.Critical(), "failed", err)
			if cap.Critical() {
				return err
			}
			continue
		}
		cfg.capabilityReport.Report(ctx, cap.Name(), cap.Critical(), "ready", nil)
	}

	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			continue
		}
		if err := cap.Validate(ctx, k, workspaceDir, cfg); err != nil {
			cfg.capabilityReport.Report(ctx, "runtime-validate", true, "failed", err)
			return err
		}
	}
	cfg.capabilityReport.Report(ctx, "runtime-validate", true, "ready", nil)

	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			continue
		}
		if err := cap.Activate(ctx, k, workspaceDir, cfg); err != nil {
			cfg.capabilityReport.Report(ctx, cap.Name(), cap.Critical(), "failed", err)
			if cap.Critical() {
				return err
			}
		}
	}
	cfg.capabilityReport.Report(ctx, "runtime-activate", true, "ready", nil)
	return nil
}

type builtinToolsCapability struct{}

func (builtinToolsCapability) Name() string            { return "builtin-tools" }
func (builtinToolsCapability) Critical() bool          { return true }
func (builtinToolsCapability) Enabled(cfg config) bool { return cfg.builtin }
func (builtinToolsCapability) Register(ctx context.Context, k *kernel.Kernel, _ string, cfg config) error {
	return setupBuiltinTools(ctx, k, cfg)
}
func (builtinToolsCapability) Validate(_ context.Context, k *kernel.Kernel, _ string, cfg config) error {
	if _, ok := SkillsManager(k).Get("builtin-tools"); !ok {
		return fmt.Errorf("runtime validation failed: builtin-tools provider missing")
	}
	return nil
}
func (builtinToolsCapability) Activate(context.Context, *kernel.Kernel, string, config) error {
	return nil
}

type mcpCapability struct{}

func (mcpCapability) Name() string            { return "mcp" }
func (mcpCapability) Critical() bool          { return false }
func (mcpCapability) Enabled(cfg config) bool { return cfg.mcpServers }
func (mcpCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	return setupMCPServers(ctx, k, workspaceDir, cfg)
}
func (mcpCapability) Validate(context.Context, *kernel.Kernel, string, config) error { return nil }
func (mcpCapability) Activate(context.Context, *kernel.Kernel, string, config) error { return nil }

type skillsCapability struct{}

func (skillsCapability) Name() string            { return "skills" }
func (skillsCapability) Critical() bool          { return true }
func (skillsCapability) Enabled(cfg config) bool { return cfg.skills }
func (skillsCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	return setupSkills(ctx, k, workspaceDir, cfg)
}
func (skillsCapability) Validate(context.Context, *kernel.Kernel, string, config) error { return nil }
func (skillsCapability) Activate(context.Context, *kernel.Kernel, string, config) error { return nil }

type agentsCapability struct{}

func (agentsCapability) Name() string            { return "agents" }
func (agentsCapability) Critical() bool          { return false }
func (agentsCapability) Enabled(cfg config) bool { return cfg.agents }
func (agentsCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	setupAgents(ctx, k, workspaceDir, cfg)
	return nil
}
func (agentsCapability) Validate(context.Context, *kernel.Kernel, string, config) error { return nil }
func (agentsCapability) Activate(context.Context, *kernel.Kernel, string, config) error { return nil }

var _ runtimeCapability = builtinToolsCapability{}
var _ runtimeCapability = mcpCapability{}
var _ runtimeCapability = skillsCapability{}
var _ runtimeCapability = agentsCapability{}
