package defaults

import (
	"context"

	toolbuiltins "github.com/mossagents/moss/extensions/toolbuiltins"
	"github.com/mossagents/moss/skill"
)

// coreToolSkill 将核心工具包装为 skill.Provider，归属于 defaults 扩展层。
type coreToolSkill struct {
	toolNames []string
}

var _ skill.Provider = (*coreToolSkill)(nil)

func (s *coreToolSkill) Metadata() skill.Metadata {
	return skill.Metadata{
		Name:        "core",
		Version:     "0.3.0",
		Description: "Built-in filesystem, command execution, and user interaction tools",
		Tools:       s.toolNames,
		Prompts: []string{
			"You have access to built-in tools: read_file, write_file, list_files, search_text, run_command, ask_user.",
		},
	}
}

func (s *coreToolSkill) Init(ctx context.Context, deps skill.Deps) error {
	s.toolNames = toolbuiltins.RegisteredToolNames(deps.Sandbox, deps.Workspace, deps.Executor)
	return toolbuiltins.RegisterAll(deps.ToolRegistry, deps.Sandbox, deps.UserIO, deps.Workspace, deps.Executor)
}

func (s *coreToolSkill) Shutdown(_ context.Context) error {
	return nil
}
