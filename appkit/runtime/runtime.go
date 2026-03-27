package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	toolbuiltins "github.com/mossagents/moss/extensions/toolbuiltins"
	"github.com/mossagents/moss/extensions/agentsx"
	"github.com/mossagents/moss/extensions/skillsx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/mcp"
	"github.com/mossagents/moss/skill"
)

type config struct {
	builtin         bool
	mcpServers      bool
	skills          bool
	progressive     bool
	agents          bool
	sessionStore    session.SessionStore
	sessionStoreSet bool
	planning        bool
}

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
func WithAgents(enabled bool) Option       { return func(c *config) { c.agents = enabled } }
func WithPlanning(enabled bool) Option     { return func(c *config) { c.planning = enabled } }
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

func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	cfg, err := resolve(opts...)
	if err != nil {
		return err
	}
	logger := logging.GetLogger()
	deps := skillsx.Deps(k)

	if cfg.builtin {
		if err := skillsx.Manager(k).Register(ctx, &coreToolSkill{}, deps); err != nil {
			return err
		}
	}

	if cfg.mcpServers {
		globalCfg, _ := appconfig.LoadGlobalConfig()
		projectCfg, _ := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspaceDir))
		merged := appconfig.MergeConfigs(globalCfg, projectCfg)

		for _, sc := range merged.Skills {
			if !sc.IsEnabled() || !sc.IsMCP() {
				continue
			}
			mcpServer := mcp.NewMCPServer(sc)
			if err := skillsx.Manager(k).Register(ctx, mcpServer, deps); err != nil {
				logger.WarnContext(ctx, "failed to load MCP server",
					slog.String("server", sc.Name),
					slog.Any("error", err),
				)
			}
		}
	}

	if cfg.skills {
		manifests := skill.DiscoverSkillManifests(workspaceDir)
		if cfg.progressive {
			skillsx.SetManifests(k, manifests)
			skillsx.EnableProgressive(k)
			if err := skillsx.RegisterProgressiveTools(k); err != nil {
				return err
			}
		} else {
			for _, mf := range manifests {
				ps, err := skill.ParseSkillMD(mf.Source)
				if err != nil {
					logger.WarnContext(ctx, "failed to parse skill",
						slog.String("source", mf.Source),
						slog.Any("error", err),
					)
					continue
				}
				if err := skillsx.Manager(k).Register(ctx, ps, deps); err != nil {
					logger.WarnContext(ctx, "failed to load skill",
						slog.String("skill", ps.Metadata().Name),
						slog.Any("error", err),
					)
				}
			}
		}
	}

	if cfg.agents {
		agentDirs := []string{
			filepath.Join(workspaceDir, ".agents", "agents"),
		}
		if home, err := os.UserHomeDir(); err == nil {
			agentDirs = append(agentDirs, filepath.Join(home, ".moss", "agents"))
		}
		registry := agentsx.Registry(k)
		for _, dir := range agentDirs {
			if err := registry.LoadDir(dir); err != nil {
				logger.WarnContext(ctx, "failed to load agents",
					slog.String("dir", dir),
					slog.Any("error", err),
				)
			}
		}
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

type coreToolSkill struct {
	toolNames []string
}

func (s *coreToolSkill) Metadata() skill.Metadata {
	return skill.Metadata{
		Name:        "core",
		Version:     "0.3.0",
		Description: "Built-in filesystem editing/search, command execution, and user interaction tools",
		Tools:       s.toolNames,
		Prompts: []string{
			"You have access to built-in tools: read_file, write_file, edit_file, glob, list_files, grep, run_command, ask_user.",
		},
	}
}

func (s *coreToolSkill) Init(ctx context.Context, deps skill.Deps) error {
	s.toolNames = toolbuiltins.RegisteredToolNames(deps.Sandbox, deps.Workspace, deps.Executor)
	return toolbuiltins.RegisterAll(deps.ToolRegistry, deps.Sandbox, deps.UserIO, deps.Workspace, deps.Executor)
}

func (s *coreToolSkill) Shutdown(_ context.Context) error { return nil }
