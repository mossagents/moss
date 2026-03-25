package tui

import (
	_ "embed"
	"strings"

	appconfig "github.com/mossagi/moss/kernel/config"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

// buildSystemPrompt 构造 Agent 的 system prompt。
// 风格：类 Claude Code / Cursor 的通用编程助手。
// skillPrompts 是来自 SkillManager 的额外提示片段。
func buildSystemPrompt(workspace string, skillPrompts ...string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	base := appconfig.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, ctx)

	if len(skillPrompts) > 0 {
		base += "\n## Additional Skills\n" + strings.Join(skillPrompts, "\n")
	}

	return base
}
