package prompting

import (
	"strings"

	"github.com/mossagents/moss/kernel"
)

func BuildSystemPrompt(workspace, trust string, k *kernel.Kernel, skillPrompts ...string) (string, error) {
	out, err := Compose(ComposeInput{
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

func ComposeSystemPrompt(workspace, trust string, k *kernel.Kernel, configInstructions, modelInstructions string, metadata map[string]any, skillPrompts ...string) (string, error) {
	sessionInstructions, err := SessionInstructionsFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	profileName, taskMode, err := ProfileModeFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	out, err := Compose(ComposeInput{
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
	AttachComposeDebugMeta(metadata, out.DebugMeta)
	return strings.TrimSpace(out.Prompt), nil
}
