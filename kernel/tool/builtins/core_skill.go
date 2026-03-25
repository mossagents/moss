package builtins

import (
	"context"

	"github.com/mossagi/moss/kernel/skill"
)

// BuiltinTool 将内置 6 个工具包装为 Provider 接口。
type BuiltinTool struct {
	toolNames []string
}

var _ skill.Provider = (*BuiltinTool)(nil)

func (s *BuiltinTool) Metadata() skill.Metadata {
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

func (s *BuiltinTool) Init(ctx context.Context, deps skill.Deps) error {
	s.toolNames = RegisteredToolNames(deps.Sandbox, deps.Workspace, deps.Executor)
	return RegisterAll(deps.ToolRegistry, deps.Sandbox, deps.UserIO, deps.Workspace, deps.Executor)
}

func (s *BuiltinTool) Shutdown(_ context.Context) error {
	return nil
}
