package kernel

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mossagi/moss/kernel/agent"
	appconfig "github.com/mossagi/moss/kernel/config"
	"github.com/mossagi/moss/kernel/logging"
	"github.com/mossagi/moss/kernel/skill"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

// SetupOption 控制 SetupWithDefaults 的行为。
type SetupOption func(*setupConfig)

type setupConfig struct {
	builtin    bool
	mcpServers bool
	skills     bool
}

// WithoutBuiltin 禁用内置核心工具注册。
func WithoutBuiltin() SetupOption {
	return func(c *setupConfig) { c.builtin = false }
}

// WithoutMCPServers 禁用 MCP server 自动加载。
func WithoutMCPServers() SetupOption {
	return func(c *setupConfig) { c.mcpServers = false }
}

// WithoutSkills 禁用 SKILL.md 自动发现。
func WithoutSkills() SetupOption {
	return func(c *setupConfig) { c.skills = false }
}

// SetupWithDefaults 注册标准技能（BuiltinTool、MCP servers、Skills）。
// 这是库用户推荐的快速开始方式，一行代码替代手动注册流程。
//
// 默认行为:
//   - 注册 6 个内置工具（read_file, write_file, list_files, search_text, run_command, ask_user）
//   - 从 ~/.moss/config.yaml 和 ./moss.yaml 加载 MCP servers
//   - 从标准目录发现 SKILL.md skills
//
// 可通过 SetupOption 选择性禁用：
//
//	k.SetupWithDefaults(ctx, ".", kernel.WithoutMCPServers(), kernel.WithoutSkills())
func (k *Kernel) SetupWithDefaults(ctx context.Context, workspaceDir string, opts ...SetupOption) error {
	cfg := &setupConfig{
		builtin:    true,
		mcpServers: true,
		skills:     true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	logger := logging.GetLogger()
	deps := k.SkillDeps()

	// 1. 注册内置工具 skill
	if cfg.builtin {
		if err := k.skills.Register(ctx, &toolbuiltins.BuiltinTool{}, deps); err != nil {
			return err
		}
	}

	// 2. 加载配置文件中的 MCP servers
	if cfg.mcpServers {
		globalCfg, _ := appconfig.LoadGlobalConfig()
		projectCfg, _ := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspaceDir))
		merged := appconfig.MergeConfigs(globalCfg, projectCfg)

		for _, sc := range merged.Skills {
			if !sc.IsEnabled() || !sc.IsMCP() {
				continue
			}
			mcpServer := skill.NewMCPServer(sc)
			if err := k.skills.Register(ctx, mcpServer, deps); err != nil {
				logger.WarnContext(ctx, "failed to load MCP server",
					slog.String("server", sc.Name),
					slog.Any("error", err),
				)
			}
		}
	}

	// 3. 发现并加载 SKILL.md skills
	if cfg.skills {
		skills := skill.DiscoverSkills(workspaceDir)
		for _, ps := range skills {
			if err := k.skills.Register(ctx, ps, deps); err != nil {
				logger.WarnContext(ctx, "failed to load skill",
					slog.String("skill", ps.Metadata().Name),
					slog.Any("error", err),
				)
			}
		}
	}

	// 4. 发现并加载 Agent 配置（.agents/agents/ 目录）
	if k.agents == nil {
		k.agents = agent.NewRegistry()
	}
	agentDirs := []string{
		filepath.Join(workspaceDir, ".agents", "agents"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		agentDirs = append(agentDirs, filepath.Join(home, ".moss", "agents"))
	}
	for _, dir := range agentDirs {
		if err := k.agents.LoadDir(dir); err != nil {
			logger.WarnContext(ctx, "failed to load agents",
				slog.String("dir", dir),
				slog.Any("error", err),
			)
		}
	}

	return nil
}
