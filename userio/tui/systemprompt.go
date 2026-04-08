package tui

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/userio/prompting"
	"strings"
)

// buildSystemPrompt 构造 Agent 的 system prompt。
// skillPrompts 是来自 SkillManager 的额外提示片段。
func buildSystemPrompt(workspace, trust string, k *kernel.Kernel, skillPrompts ...string) (string, error) {
	out, err := prompting.Compose(prompting.ComposeInput{
		Workspace:    workspace,
		Trust:        trust,
		Kernel:       k,
		SkillPrompts: skillPrompts,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Prompt), nil
}

func composeSystemPrompt(workspace, trust string, k *kernel.Kernel, configInstructions, modelInstructions string, metadata map[string]any, skillPrompts ...string) (string, error) {
	sessionInstructions, err := prompting.SessionInstructionsFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	profileName, taskMode, err := prompting.ProfileModeFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	out, err := prompting.Compose(prompting.ComposeInput{
		Workspace:           workspace,
		Trust:               trust,
		ConfigInstructions:  strings.TrimSpace(configInstructions),
		SessionInstructions: sessionInstructions,
		ModelInstructions:   strings.TrimSpace(modelInstructions),
		ProfileName:         profileName,
		TaskMode:            taskMode,
		Kernel:              k,
		SkillPrompts:        skillPrompts,
	})
	if err != nil {
		return "", err
	}
	prompting.AttachComposeDebugMeta(metadata, out.DebugMeta)
	return strings.TrimSpace(out.Prompt), nil
}
