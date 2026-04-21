package prompting

import (
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
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
	taskMode, err := taskModeFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	out, err := Compose(ComposeInput{
		Workspace:           workspace,
		Trust:               trust,
		ConfigInstructions:  strings.TrimSpace(configInstructions),
		SessionInstructions: sessionInstructions,
		ModelInstructions:   strings.TrimSpace(modelInstructions),
		TaskMode:            taskMode,
		CollaborationMode:   effectiveCollaborationMode("", taskMode, ""),
		Kernel:              k,
		SkillPrompts:        skillPrompts,
	})
	if err != nil {
		return "", err
	}
	AttachComposeDebugMeta(metadata, out.DebugMeta)
	return strings.TrimSpace(out.Prompt), nil
}

func ComposeSystemPromptForConfig(workspace, trust string, k *kernel.Kernel, configInstructions, modelInstructions string, cfg session.SessionConfig, skillPrompts ...string) (string, map[string]any, error) {
	metadata := clonePromptMetadata(cfg.Metadata)
	cfg.Metadata = metadata
	sessionInstructions, err := SessionInstructionsFromMetadata(metadata)
	if err != nil {
		return "", nil, err
	}
	mode, err := SessionPromptModeFromConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	out, err := Compose(ComposeInput{
		Workspace:           workspace,
		Trust:               trust,
		ConfigInstructions:  strings.TrimSpace(configInstructions),
		SessionInstructions: sessionInstructions,
		ModelInstructions:   strings.TrimSpace(modelInstructions),
		ProfileName:         mode.ProfileName,
		TaskMode:            mode.TaskMode,
		CollaborationMode:   mode.CollaborationMode,
		PermissionProfile:   mode.PermissionProfile,
		PromptPack:          mode.PromptPack,
		Preset:              mode.Preset,
		Kernel:              k,
		SkillPrompts:        skillPrompts,
	})
	if err != nil {
		return "", nil, err
	}
	AttachComposeDebugMeta(metadata, out.DebugMeta)
	return strings.TrimSpace(out.Prompt), metadata, nil
}

func clonePromptMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
