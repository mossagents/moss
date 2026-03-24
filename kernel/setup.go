package kernel

import (
	"context"
	"fmt"
	"io"

	"github.com/mossagi/moss/kernel/skill"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

// SetupOption 控制 SetupWithDefaults 的行为。
type SetupOption func(*setupConfig)

type setupConfig struct {
	builtin    bool
	mcpServers bool
	skills     bool
	warnWriter io.Writer
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

// WithWarningWriter 设置加载警告的输出目标，默认丢弃。
func WithWarningWriter(w io.Writer) SetupOption {
	return func(c *setupConfig) { c.warnWriter = w }
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

	deps := k.SkillDeps()

	// 1. 注册内置工具 skill
	if cfg.builtin {
		if err := k.skills.Register(ctx, &toolbuiltins.BuiltinTool{}, deps); err != nil {
			return fmt.Errorf("register builtin tool: %w", err)
		}
	}

	// 2. 加载配置文件中的 MCP servers
	if cfg.mcpServers {
		globalCfg, _ := skill.LoadGlobalConfig()
		projectCfg, _ := skill.LoadConfig(skill.DefaultProjectConfigPath(workspaceDir))
		merged := skill.MergeConfigs(globalCfg, projectCfg)

		for _, sc := range merged.Skills {
			if !sc.IsEnabled() || !sc.IsMCP() {
				continue
			}
			mcpServer := skill.NewMCPServer(sc)
			if err := k.skills.Register(ctx, mcpServer, deps); err != nil {
				if cfg.warnWriter != nil {
					fmt.Fprintf(cfg.warnWriter, "warning: failed to load MCP server %q: %v\n", sc.Name, err)
				}
			}
		}
	}

	// 3. 发现并加载 SKILL.md skills
	if cfg.skills {
		skills := skill.DiscoverSkills(workspaceDir)
		for _, ps := range skills {
			if err := k.skills.Register(ctx, ps, deps); err != nil {
				if cfg.warnWriter != nil {
					fmt.Fprintf(cfg.warnWriter, "warning: failed to load skill %q: %v\n", ps.Metadata().Name, err)
				}
			}
		}
	}

	return nil
}
