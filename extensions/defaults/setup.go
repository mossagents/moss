package defaults

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/extensions/agentsx"
	"github.com/mossagents/moss/extensions/skillsx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/mcp"
	"github.com/mossagents/moss/skill"
)

// Option 控制默认扩展装配行为。
type Option func(*config)

type config struct {
	builtin    bool
	mcpServers bool
	skills     bool
}

// WithoutBuiltin 禁用内置核心工具注册。
func WithoutBuiltin() Option {
	return func(c *config) { c.builtin = false }
}

// WithoutMCPServers 禁用 MCP server 自动加载。
func WithoutMCPServers() Option {
	return func(c *config) { c.mcpServers = false }
}

// WithoutSkills 禁用 SKILL.md 自动发现。
func WithoutSkills() Option {
	return func(c *config) { c.skills = false }
}

// Setup 装配官方默认扩展（BuiltinTool、MCP servers、Skills、Agent configs）。
// 这是推荐的快速开始方式，但归属于扩展层而非 kernel core。
//
// 默认行为:
//   - 注册 8 个内置工具（read_file, write_file, edit_file, glob, list_files, search_text, run_command, ask_user）
//   - 从 ~/.moss/config.yaml 和 ./moss.yaml 加载 MCP servers
//   - 从标准目录发现 SKILL.md skills
//   - 从标准目录加载 Agent 配置
//
// 可通过 Option 选择性禁用：
//
//	defaults.Setup(ctx, k, ".", defaults.WithoutMCPServers(), defaults.WithoutSkills())
func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	cfg := &config{
		builtin:    true,
		mcpServers: true,
		skills:     true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	logger := logging.GetLogger()
	deps := skillsx.Deps(k)

	// 1. 注册内置工具 skill
	if cfg.builtin {
		if err := skillsx.Manager(k).Register(ctx, &coreToolSkill{}, deps); err != nil {
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
			mcpServer := mcp.NewMCPServer(sc)
			if err := skillsx.Manager(k).Register(ctx, mcpServer, deps); err != nil {
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
			if err := skillsx.Manager(k).Register(ctx, ps, deps); err != nil {
				logger.WarnContext(ctx, "failed to load skill",
					slog.String("skill", ps.Metadata().Name),
					slog.Any("error", err),
				)
			}
		}
	}

	// 4. 发现并加载 Agent 配置（.agents/agents/ 目录）
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

	return nil
}
