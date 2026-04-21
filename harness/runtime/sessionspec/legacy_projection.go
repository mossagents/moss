package sessionspec

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/harness/runtime/collaboration"
	"github.com/mossagents/moss/harness/runtime/permissions"
	runtimepolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/harness/runtime/profile"
	"github.com/mossagents/moss/harness/runtime/promptpacks"
	kernelsession "github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

type LegacyResolveOptions struct {
	Workspace        string
	RequestedProfile string
	Trust            string
	ApprovalMode     string
}

type LegacyRuntimeSelection struct {
	Name         string
	TaskMode     string
	Trust        string
	ApprovalMode string
	ToolPolicy   runtimepolicy.ToolPolicy
}

type LegacyProjectionInput struct {
	PromptPack string
	Provider   string
	ModelName  string
}

func ResolveLegacyRuntimeSelection(opts LegacyResolveOptions) (LegacyRuntimeSelection, error) {
	resolved, err := profile.ResolveProfileForWorkspace(profile.ProfileResolveOptions{
		Workspace:        opts.Workspace,
		RequestedProfile: opts.RequestedProfile,
		Trust:            opts.Trust,
		ApprovalMode:     opts.ApprovalMode,
	})
	if err != nil {
		return LegacyRuntimeSelection{}, err
	}
	return legacyRuntimeSelectionFromResolvedProfile(resolved), nil
}

func DefaultLegacyRuntimeSelection(workspace, requestedProfile, trust, approvalMode string) LegacyRuntimeSelection {
	profileName := strings.TrimSpace(requestedProfile)
	if profileName == "" {
		profileName = "default"
	}
	return LegacyRuntimeSelection{
		Name:         profileName,
		TaskMode:     profileName,
		Trust:        strings.TrimSpace(trust),
		ApprovalMode: strings.TrimSpace(approvalMode),
		ToolPolicy:   runtimepolicy.ResolveToolPolicyForWorkspace(workspace, trust, approvalMode),
	}
}

func ApplyLegacyRuntimeSelection(cfg kernelsession.SessionConfig, selection LegacyRuntimeSelection, input LegacyProjectionInput) (kernelsession.SessionConfig, error) {
	resolved := selection.resolvedProfile()
	base := profile.ApplyResolvedProfileToSessionConfig(cfg, resolved)
	projected, err := ApplyLegacyProfileProjection(base, resolved, input)
	if err != nil {
		return base, err
	}
	return projected, nil
}

func ApplyLegacyProfileProjection(cfg kernelsession.SessionConfig, resolved profile.ResolvedProfile, input LegacyProjectionInput) (kernelsession.SessionConfig, error) {
	runMode := strings.TrimSpace(cfg.Mode)
	if runMode == "" {
		runMode = "interactive"
	}
	trust := strings.TrimSpace(cfg.TrustLevel)
	if trust == "" {
		trust = strings.TrimSpace(resolved.Trust)
	}
	promptPack := strings.TrimSpace(input.PromptPack)
	if promptPack == "" {
		promptPack = "coding"
	}
	preset := strings.TrimSpace(resolved.Name)
	collaborationMode := legacyProfileCollaborationMode(resolved.TaskMode, resolved.Name)
	permissionProfileName := legacyPermissionProfileName(resolved.Name)
	sessionPolicyName := legacySessionPolicyName(runMode)
	modelProfileName := legacyModelProfileName(input.Provider, input.ModelName)
	compiledPolicy, err := permissions.Compile(permissions.Profile{
		Name:                    permissionProfileName,
		ApprovalPolicy:          strings.TrimSpace(resolved.ApprovalMode),
		Command:                 resolved.ToolPolicy.Command,
		HTTP:                    resolved.ToolPolicy.HTTP,
		WorkspaceWriteAccess:    resolved.ToolPolicy.WorkspaceWriteAccess,
		MemoryWriteAccess:       resolved.ToolPolicy.MemoryWriteAccess,
		GraphMutationAccess:     resolved.ToolPolicy.GraphMutationAccess,
		ProtectedPathPrefixes:   append([]string(nil), resolved.ToolPolicy.ProtectedPathPrefixes...),
		ApprovalRequiredClasses: append([]tool.ApprovalClass(nil), resolved.ToolPolicy.ApprovalRequiredClasses...),
		DeniedClasses:           append([]tool.ApprovalClass(nil), resolved.ToolPolicy.DeniedClasses...),
	}, trust)
	if err != nil {
		return kernelsession.SessionConfig{}, fmt.Errorf("compile legacy permission profile: %w", err)
	}
	return applyLegacyProjection(cfg, resolved, input, runMode, trust, promptPack, preset, collaborationMode, permissionProfileName, sessionPolicyName, modelProfileName, compiledPolicy)
}

func applyLegacyProjection(cfg kernelsession.SessionConfig, resolved profile.ResolvedProfile, input LegacyProjectionInput, runMode, trust, promptPack, preset, collaborationMode, permissionProfileName, sessionPolicyName, modelProfileName string, compiledPolicy permissions.CompiledPolicy) (kernelsession.SessionConfig, error) {
	var snapshot *PromptSnapshot
	if existing, ok := PromptSnapshotFromConfig(cfg); ok {
		snapshot = existing
	}
	return ApplyToSessionConfig(cfg, SessionSpec{
		Goal:      strings.TrimSpace(cfg.Goal),
		Workspace: WorkspaceRequest{Trust: trust},
		Intent: IntentRequest{
			CollaborationMode: collaborationMode,
			PromptPack:        promptPack,
		},
		Runtime: RuntimeRequest{
			RunMode:           runMode,
			PermissionProfile: permissionProfileName,
			SessionPolicy:     sessionPolicyName,
			ModelProfile:      modelProfileName,
		},
		Origin: OriginRequest{Preset: preset},
	}, ResolvedSessionSpec{
		Goal:      strings.TrimSpace(cfg.Goal),
		Workspace: ResolvedWorkspace{Trust: trust},
		Intent: ResolvedIntent{
			CollaborationMode: collaborationMode,
			PromptPack:        promptPackRef(promptPack),
			CapabilityCeiling: collaborationCapabilities(collaborationMode),
		},
		Runtime: ResolvedRuntime{
			RunMode:           runMode,
			PermissionProfile: permissionProfileName,
			PermissionPolicy:  compiledPolicy,
			SessionPolicyName: sessionPolicyName,
			SessionPolicy: SessionPolicyConfig{
				MaxSteps:             cfg.MaxSteps,
				MaxTokens:            cfg.MaxTokens,
				Timeout:              cfg.Timeout,
				AutoCompactThreshold: cfg.ModelConfig.AutoCompactTokenLimit,
			},
			ModelProfile: modelProfileName,
			Model: ModelProfile{
				Provider:    strings.TrimSpace(input.Provider),
				ModelConfig: cfg.ModelConfig,
			},
		},
		Prompt: ResolvedPrompt{BasePackID: promptPack},
		Origin: ResolvedOrigin{Preset: preset},
	}, snapshot)
}

func legacyProfileCollaborationMode(taskMode, profileName string) string {
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(taskMode, profileName))) {
	case "planning", "plan":
		return "plan"
	case "research", "investigate":
		return "investigate"
	default:
		return "execute"
	}
}

func legacyPermissionProfileName(profileName string) string {
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = "default"
	}
	return "legacy:" + sanitizeLegacyName(name)
}

func legacySessionPolicyName(runMode string) string {
	return "legacy-session:" + sanitizeLegacyName(firstNonEmpty(runMode, "interactive"))
}

func legacyModelProfileName(provider, modelName string) string {
	combined := sanitizeLegacyName(firstNonEmpty(strings.TrimSpace(provider), "default-provider"))
	modelPart := sanitizeLegacyName(firstNonEmpty(strings.TrimSpace(modelName), "default-model"))
	return "legacy-model:" + combined + ":" + modelPart
}

func sanitizeLegacyName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	if value == "" {
		return "default"
	}
	return value
}

func promptPackRef(id string) promptpacks.Pack {
	return promptpacks.Pack{ID: strings.TrimSpace(id), Source: "legacy:" + strings.TrimSpace(id)}
}

func collaborationCapabilities(mode string) []collaboration.Capability {
	return collaboration.CeilingForMode(collaboration.NormalizeMode(mode)).Slice()
}

func legacyRuntimeSelectionFromResolvedProfile(resolved profile.ResolvedProfile) LegacyRuntimeSelection {
	return LegacyRuntimeSelection{
		Name:         strings.TrimSpace(resolved.Name),
		TaskMode:     strings.TrimSpace(resolved.TaskMode),
		Trust:        strings.TrimSpace(resolved.Trust),
		ApprovalMode: strings.TrimSpace(resolved.ApprovalMode),
		ToolPolicy:   runtimepolicy.CloneToolPolicy(resolved.ToolPolicy),
	}
}

func (s LegacyRuntimeSelection) resolvedProfile() profile.ResolvedProfile {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = "default"
	}
	taskMode := strings.TrimSpace(s.TaskMode)
	if taskMode == "" {
		taskMode = name
	}
	return profile.ResolvedProfile{
		RequestedName: name,
		Name:          name,
		Label:         name,
		TaskMode:      taskMode,
		Trust:         strings.TrimSpace(s.Trust),
		ApprovalMode:  strings.TrimSpace(s.ApprovalMode),
		ToolPolicy:    runtimepolicy.CloneToolPolicy(s.ToolPolicy),
	}
}
