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
	coreSkill    bool
	mcpSkills    bool
	promptSkills bool
	warnWriter   io.Writer
}

// WithoutCoreSkill 禁用内置核心工具注册。
func WithoutCoreSkill() SetupOption {
	return func(c *setupConfig) { c.coreSkill = false }
}

// WithoutMCPSkills 禁用 MCP skill 自动加载。
func WithoutMCPSkills() SetupOption {
	return func(c *setupConfig) { c.mcpSkills = false }
}

// WithoutPromptSkills 禁用 SKILL.md prompt skill 自动发现。
func WithoutPromptSkills() SetupOption {
	return func(c *setupConfig) { c.promptSkills = false }
}

// WithWarningWriter 设置加载警告的输出目标，默认丢弃。
func WithWarningWriter(w io.Writer) SetupOption {
	return func(c *setupConfig) { c.warnWriter = w }
}

// SetupWithDefaults 注册标准技能（CoreSkill、MCP skills、PromptSkills）。
// 这是库用户推荐的快速开始方式，一行代码替代手动注册流程。
//
// 默认行为:
//   - 注册 6 个内置工具（read_file, write_file, list_files, search_text, run_command, ask_user）
//   - 从 ~/.moss/config.yaml 和 ./moss.yaml 加载 MCP skills
//   - 从标准目录发现 SKILL.md prompt skills
//
// 可通过 SetupOption 选择性禁用：
//
//	k.SetupWithDefaults(ctx, ".", kernel.WithoutMCPSkills(), kernel.WithoutPromptSkills())
func (k *Kernel) SetupWithDefaults(ctx context.Context, workspaceDir string, opts ...SetupOption) error {
	cfg := &setupConfig{
		coreSkill:    true,
		mcpSkills:    true,
		promptSkills: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	deps := k.SkillDeps()

	// 1. 注册内置工具 skill
	if cfg.coreSkill {
		if err := k.skills.Register(ctx, &toolbuiltins.CoreSkill{}, deps); err != nil {
			return fmt.Errorf("register core skill: %w", err)
		}
	}

	// 2. 加载配置文件中的 MCP skills
	if cfg.mcpSkills {
		globalCfg, _ := skill.LoadGlobalConfig()
		projectCfg, _ := skill.LoadConfig(skill.DefaultProjectConfigPath(workspaceDir))
		merged := skill.MergeConfigs(globalCfg, projectCfg)

		for _, sc := range merged.Skills {
			if !sc.IsEnabled() || !sc.IsMCP() {
				continue
			}
			mcpSkill := skill.NewMCPSkill(sc)
			if err := k.skills.Register(ctx, mcpSkill, deps); err != nil {
				if cfg.warnWriter != nil {
					fmt.Fprintf(cfg.warnWriter, "warning: failed to load MCP skill %q: %v\n", sc.Name, err)
				}
			}
		}
	}

	// 3. 发现并加载 SKILL.md prompt skills
	if cfg.promptSkills {
		promptSkills := skill.DiscoverPromptSkills(workspaceDir)
		for _, ps := range promptSkills {
			if err := k.skills.Register(ctx, ps, deps); err != nil {
				if cfg.warnWriter != nil {
					fmt.Fprintf(cfg.warnWriter, "warning: failed to load prompt skill %q: %v\n", ps.Metadata().Name, err)
				}
			}
		}
	}

	return nil
}
