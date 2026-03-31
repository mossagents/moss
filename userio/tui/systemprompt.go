package tui

import (
	_ "embed"
	"strings"

	"github.com/mossagents/moss/bootstrap"
	config "github.com/mossagents/moss/config"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

// buildSystemPrompt 构造 Agent 的 system prompt。
// 风格：类 Claude Code / Cursor 的通用编程助手。
// skillPrompts 是来自 SkillManager 的额外提示片段。
func buildSystemPrompt(workspace, trust string, skillPrompts ...string) string {
	ctx := config.DefaultTemplateContext(workspace)
	base := config.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)

	if bctx := bootstrap.LoadWithAppNameAndTrust(workspace, config.AppName(), trust); bctx != nil {
		if sec := strings.TrimSpace(bctx.SystemPromptSection()); sec != "" {
			base += "\n## Bootstrap Context\n" + sec
		}
	}

	if len(skillPrompts) > 0 {
		base += "\n## Additional Skills\n" + strings.Join(skillPrompts, "\n")
	}

	return base
}
