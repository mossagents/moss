package builtins

import (
	"context"

	"github.com/mossagi/moss/kernel/skill"
)

// CoreSkill 将内置 6 个工具包装为 Skill 接口。
type CoreSkill struct {
	toolNames []string
}

var _ skill.Skill = (*CoreSkill)(nil)

func (s *CoreSkill) Metadata() skill.Metadata {
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

func (s *CoreSkill) Init(ctx context.Context, deps skill.Deps) error {
	s.toolNames = []string{
		"read_file", "write_file", "list_files",
		"search_text", "run_command", "ask_user",
	}
	return RegisterAll(deps.ToolRegistry, deps.Sandbox, deps.UserIO)
}

func (s *CoreSkill) Shutdown(_ context.Context) error {
	return nil
}
