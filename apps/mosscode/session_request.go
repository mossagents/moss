package main

import (
	"strings"

	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/runtime/permissions"
	"github.com/mossagents/moss/harness/runtime/presets"
	"github.com/mossagents/moss/harness/runtime/promptpacks"
	rsessionspec "github.com/mossagents/moss/harness/runtime/sessionspec"
	"github.com/mossagents/moss/kernel/session"
)

type runtimeInvocation struct {
	CompatFlags   *appkit.AppFlags
	ApprovalMode  string
	RequestedSpec rsessionspec.SessionSpec
	ResolvedSpec  rsessionspec.ResolvedSessionSpec
}

func (r sessionRequest) normalized() sessionRequest {
	return sessionRequest{
		Preset:            strings.TrimSpace(r.Preset),
		CollaborationMode: strings.TrimSpace(r.CollaborationMode),
		RunMode:           strings.TrimSpace(r.RunMode),
		PermissionProfile: strings.TrimSpace(r.PermissionProfile),
		PromptPack:        strings.TrimSpace(r.PromptPack),
		SessionPolicy:     strings.TrimSpace(r.SessionPolicy),
		ModelProfile:      strings.TrimSpace(r.ModelProfile),
	}
}

func (r sessionRequest) active() bool {
	r = r.normalized()
	return r.Preset != "" ||
		r.CollaborationMode != "" ||
		r.RunMode != "" ||
		r.PermissionProfile != "" ||
		r.PromptPack != "" ||
		r.SessionPolicy != "" ||
		r.ModelProfile != ""
}

func resolveRuntimeInvocation(cfg *config, defaultRunMode string) (runtimeInvocation, error) {
	request := cfg.request.normalized()
	requested := rsessionspec.SessionSpec{
		Workspace: rsessionspec.WorkspaceRequest{Trust: strings.TrimSpace(cfg.flags.Trust)},
		Intent: rsessionspec.IntentRequest{
			CollaborationMode: request.CollaborationMode,
			PromptPack:        request.PromptPack,
		},
		Runtime: rsessionspec.RuntimeRequest{
			RunMode:           request.RunMode,
			PermissionProfile: request.PermissionProfile,
			SessionPolicy:     request.SessionPolicy,
			ModelProfile:      request.ModelProfile,
		},
		Origin: rsessionspec.OriginRequest{Preset: request.Preset},
	}

	resolved, err := rsessionspec.Resolve(requested, typedResolveInput(cfg.flags, defaultRunMode))
	if err != nil {
		return runtimeInvocation{}, err
	}

	compatFlags := cloneAppFlags(cfg.flags)
	compatFlags.Trust = strings.TrimSpace(resolved.Workspace.Trust)
	compatFlags.Provider = firstNonEmptyTrimmed(strings.TrimSpace(resolved.Runtime.Model.Provider), compatFlags.Provider)
	compatFlags.Model = firstNonEmptyTrimmed(strings.TrimSpace(resolved.Runtime.Model.ModelConfig.Model), compatFlags.Model)

	return runtimeInvocation{
		CompatFlags:   compatFlags,
		ApprovalMode:  strings.TrimSpace(resolved.Runtime.PermissionPolicy.Policy.ApprovalMode),
		RequestedSpec: requested,
		ResolvedSpec:  resolved,
	}, nil
}

func typedResolveInput(flags *appkit.AppFlags, defaultRunMode string) rsessionspec.ResolveInput {
	resolvedFlags := cloneAppFlags(flags)
	activeModel := buildActiveModelProfile(resolvedFlags)
	return rsessionspec.ResolveInput{
		WorkspaceTrust: firstNonEmptyTrimmed(resolvedFlags.Trust, "restricted"),
		DefaultRunMode: defaultRunMode,
		GlobalDefaults: rsessionspec.Defaults{Preset: "code"},
		Registries: rsessionspec.Registries{
			PromptPacks: map[string]promptpacks.Pack{
				"coding": {ID: "coding", Source: "builtin:coding"},
			},
			Presets: map[string]presets.Preset{
				"code": {
					ID:                "code",
					PromptPack:        "coding",
					CollaborationMode: "execute",
					PermissionProfile: "workspace-write",
					SessionPolicy:     "deep-work",
					ModelProfile:      "code-default",
				},
			},
			PermissionProfiles: map[string]permissions.Profile{
				"read-only":       {Name: "read-only", ApprovalPolicy: "read-only"},
				"workspace-write": {Name: "workspace-write", ApprovalPolicy: "confirm"},
				"full-auto":       {Name: "full-auto", ApprovalPolicy: "full-auto"},
			},
			SessionPolicies: map[string]rsessionspec.SessionPolicyConfig{
				"deep-work": {MaxSteps: 200, MaxTokens: 120000},
			},
			ModelProfiles: map[string]rsessionspec.ModelProfile{
				"active":       activeModel,
				"code-default": activeModel,
			},
		},
	}
}

func buildActiveModelProfile(flags *appkit.AppFlags) rsessionspec.ModelProfile {
	resolvedFlags := cloneAppFlags(flags)
	base := applyContextPolicy(session.SessionConfig{}, resolvedFlags)
	return rsessionspec.ModelProfile{
		Provider:    strings.TrimSpace(resolvedFlags.Provider),
		ModelConfig: base.ModelConfig,
	}
}

func buildTypedProjectedSessionConfig(base session.SessionConfig, flags *appkit.AppFlags, invocation runtimeInvocation) session.SessionConfig {
	requested := invocation.RequestedSpec
	requested.Goal = strings.TrimSpace(base.Goal)
	resolved := invocation.ResolvedSpec
	resolved.Goal = strings.TrimSpace(base.Goal)
	cfg := applyContextPolicy(base, flags)
	projected, err := rsessionspec.ApplyToSessionConfig(cfg, requested, resolved, nil)
	if err != nil {
		return cfg
	}
	return projected
}

func cloneAppFlags(flags *appkit.AppFlags) *appkit.AppFlags {
	if flags == nil {
		return &appkit.AppFlags{}
	}
	copy := *flags
	return &copy
}

func permissionProfileFromApprovalMode(approvalMode string) string {
	switch strings.ToLower(strings.TrimSpace(approvalMode)) {
	case "read-only", "readonly", "ro":
		return "read-only"
	case "full-auto", "full", "auto":
		return "full-auto"
	default:
		return "workspace-write"
	}
}

func defaultCollaborationModeForInvocation(invocation runtimeInvocation) string {
	if mode := strings.TrimSpace(invocation.ResolvedSpec.Intent.CollaborationMode); mode != "" {
		return mode
	}
	return "execute"
}

func runtimeInvocationForTUI(base runtimeInvocation, flags *appkit.AppFlags, approvalMode, collaborationMode, runMode string) (runtimeInvocation, error) {
	requested := base.RequestedSpec
	requested.Workspace.Trust = strings.TrimSpace(flags.Trust)
	requested.Goal = ""
	requested.Intent.CollaborationMode = strings.TrimSpace(collaborationMode)
	requested.Runtime.RunMode = strings.TrimSpace(runMode)
	if requested.Runtime.PermissionProfile == "" && strings.TrimSpace(approvalMode) != "" {
		requested.Runtime.PermissionProfile = permissionProfileFromApprovalMode(approvalMode)
	}
	resolved, err := rsessionspec.Resolve(requested, typedResolveInput(flags, runMode))
	if err != nil {
		return runtimeInvocation{}, err
	}
	compatFlags := cloneAppFlags(flags)
	compatFlags.Trust = strings.TrimSpace(resolved.Workspace.Trust)
	compatFlags.Provider = firstNonEmptyTrimmed(strings.TrimSpace(resolved.Runtime.Model.Provider), compatFlags.Provider)
	compatFlags.Model = firstNonEmptyTrimmed(strings.TrimSpace(resolved.Runtime.Model.ModelConfig.Model), compatFlags.Model)
	return runtimeInvocation{
		CompatFlags:   compatFlags,
		ApprovalMode:  strings.TrimSpace(resolved.Runtime.PermissionPolicy.Policy.ApprovalMode),
		RequestedSpec: requested,
		ResolvedSpec:  resolved,
	}, nil
}
