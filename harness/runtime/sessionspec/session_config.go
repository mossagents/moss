package sessionspec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/harness/runtime/collaboration"
	"github.com/mossagents/moss/harness/runtime/permissions"
	"github.com/mossagents/moss/harness/runtime/promptpacks"
	kernelsession "github.com/mossagents/moss/kernel/session"
)

func PersistedSessionSpec(spec SessionSpec) *kernelsession.SessionSpec {
	return &kernelsession.SessionSpec{
		Workspace: kernelsession.SessionWorkspace{Trust: strings.TrimSpace(spec.Workspace.Trust)},
		Intent: kernelsession.SessionIntent{
			CollaborationMode: strings.TrimSpace(spec.Intent.CollaborationMode),
			PromptPack:        strings.TrimSpace(spec.Intent.PromptPack),
		},
		Runtime: kernelsession.SessionRuntime{
			RunMode:           strings.TrimSpace(spec.Runtime.RunMode),
			PermissionProfile: strings.TrimSpace(spec.Runtime.PermissionProfile),
			SessionPolicy:     strings.TrimSpace(spec.Runtime.SessionPolicy),
			ModelProfile:      strings.TrimSpace(spec.Runtime.ModelProfile),
		},
		Origin: kernelsession.SessionOrigin{Preset: strings.TrimSpace(spec.Origin.Preset)},
	}
}

func PersistedResolvedSessionSpec(spec ResolvedSessionSpec) (*kernelsession.ResolvedSessionSpec, error) {
	policyJSON, err := json.Marshal(spec.Runtime.PermissionPolicy)
	if err != nil {
		return nil, fmt.Errorf("marshal compiled permission policy: %w", err)
	}
	return &kernelsession.ResolvedSessionSpec{
		Workspace: kernelsession.ResolvedWorkspace{Trust: strings.TrimSpace(spec.Workspace.Trust)},
		Intent: kernelsession.ResolvedIntent{
			CollaborationMode: strings.TrimSpace(spec.Intent.CollaborationMode),
			PromptPack: kernelsession.PromptPackRef{
				ID:     strings.TrimSpace(spec.Intent.PromptPack.ID),
				Source: strings.TrimSpace(spec.Intent.PromptPack.Source),
			},
			CapabilityCeiling: capabilityStrings(spec.Intent.CapabilityCeiling),
		},
		Runtime: kernelsession.ResolvedRuntime{
			RunMode:           strings.TrimSpace(spec.Runtime.RunMode),
			PermissionProfile: strings.TrimSpace(spec.Runtime.PermissionProfile),
			PermissionPolicy:  policyJSON,
			SessionPolicyName: strings.TrimSpace(spec.Runtime.SessionPolicyName),
			SessionPolicy: kernelsession.SessionPolicySpec{
				MaxSteps:             spec.Runtime.SessionPolicy.MaxSteps,
				MaxTokens:            spec.Runtime.SessionPolicy.MaxTokens,
				Timeout:              spec.Runtime.SessionPolicy.Timeout,
				AutoCompactThreshold: spec.Runtime.SessionPolicy.AutoCompactThreshold,
				AsyncTaskLimit:       spec.Runtime.SessionPolicy.AsyncTaskLimit,
			},
			ModelProfile:    strings.TrimSpace(spec.Runtime.ModelProfile),
			Provider:        strings.TrimSpace(spec.Runtime.Model.Provider),
			ModelConfig:     spec.Runtime.Model.ModelConfig,
			ReasoningEffort: strings.TrimSpace(spec.Runtime.Model.ReasoningEffort),
			Verbosity:       strings.TrimSpace(spec.Runtime.Model.Verbosity),
			RouterLane:      strings.TrimSpace(spec.Runtime.Model.RouterLane),
		},
		Prompt: kernelsession.ResolvedPrompt{
			BasePackID:                 strings.TrimSpace(spec.Prompt.BasePackID),
			TrustedAugmentationIDs:     append([]string(nil), spec.Prompt.TrustedAugmentationIDs...),
			TrustedAugmentationDigests: append([]string(nil), spec.Prompt.TrustedAugmentationDigests...),
			RenderedPromptVersion:      strings.TrimSpace(spec.Prompt.RenderedPromptVersion),
			SnapshotRef:                strings.TrimSpace(spec.Prompt.SnapshotRef),
		},
		Origin: kernelsession.ResolvedOrigin{Preset: strings.TrimSpace(spec.Origin.Preset)},
	}, nil
}

func PersistedPromptSnapshot(snapshot *PromptSnapshot) *kernelsession.PromptSnapshot {
	if snapshot == nil {
		return nil
	}
	layers := make([]kernelsession.ResolvedPromptLayer, 0, len(snapshot.Layers))
	for _, layer := range snapshot.Layers {
		layers = append(layers, kernelsession.ResolvedPromptLayer{
			ID:      strings.TrimSpace(layer.ID),
			Source:  strings.TrimSpace(layer.Source),
			Content: layer.Content,
		})
	}
	return &kernelsession.PromptSnapshot{
		Ref:            strings.TrimSpace(snapshot.Ref),
		Layers:         layers,
		RenderedPrompt: snapshot.RenderedPrompt,
		Version:        strings.TrimSpace(snapshot.Version),
	}
}

func ApplyToSessionConfig(cfg kernelsession.SessionConfig, spec SessionSpec, resolved ResolvedSessionSpec, snapshot *PromptSnapshot) (kernelsession.SessionConfig, error) {
	persistedResolved, err := PersistedResolvedSessionSpec(resolved)
	if err != nil {
		return kernelsession.SessionConfig{}, err
	}
	cfg.Goal = firstNonEmpty(strings.TrimSpace(resolved.Goal), strings.TrimSpace(spec.Goal), strings.TrimSpace(cfg.Goal))
	cfg.Mode = firstNonEmpty(strings.TrimSpace(resolved.Runtime.RunMode), strings.TrimSpace(spec.Runtime.RunMode), strings.TrimSpace(cfg.Mode))
	cfg.TrustLevel = firstNonEmpty(strings.TrimSpace(resolved.Workspace.Trust), strings.TrimSpace(spec.Workspace.Trust), strings.TrimSpace(cfg.TrustLevel))
	cfg.MaxSteps = maxNonZero(resolved.Runtime.SessionPolicy.MaxSteps, cfg.MaxSteps)
	cfg.MaxTokens = maxNonZero(resolved.Runtime.SessionPolicy.MaxTokens, cfg.MaxTokens)
	cfg.BudgetPolicy = firstNonEmpty(strings.TrimSpace(resolved.Runtime.SessionPolicyName), strings.TrimSpace(spec.Runtime.SessionPolicy), strings.TrimSpace(cfg.BudgetPolicy))
	if resolved.Runtime.SessionPolicy.Timeout > 0 {
		cfg.Timeout = resolved.Runtime.SessionPolicy.Timeout
	}
	cfg.ModelConfig = resolved.Runtime.Model.ModelConfig
	cfg.SessionSpec = PersistedSessionSpec(spec)
	cfg.ResolvedSessionSpec = persistedResolved
	cfg.PromptSnapshot = PersistedPromptSnapshot(snapshot)

	meta := cloneMetadata(cfg.Metadata)
	meta[kernelsession.MetadataEffectiveTrust] = strings.TrimSpace(resolved.Workspace.Trust)
	meta[kernelsession.MetadataEffectiveApproval] = strings.TrimSpace(resolved.Runtime.PermissionPolicy.Policy.ApprovalMode)
	meta[kernelsession.MetadataTaskMode] = strings.TrimSpace(resolved.Intent.CollaborationMode)
	cfg.Metadata = meta
	return cfg, nil
}

func SessionSpecFromConfig(cfg kernelsession.SessionConfig) (SessionSpec, bool) {
	if cfg.SessionSpec == nil {
		return SessionSpec{}, false
	}
	return SessionSpec{
		Goal:      strings.TrimSpace(cfg.Goal),
		Workspace: WorkspaceRequest{Trust: strings.TrimSpace(cfg.SessionSpec.Workspace.Trust)},
		Intent: IntentRequest{
			CollaborationMode: strings.TrimSpace(cfg.SessionSpec.Intent.CollaborationMode),
			PromptPack:        strings.TrimSpace(cfg.SessionSpec.Intent.PromptPack),
		},
		Runtime: RuntimeRequest{
			RunMode:           firstNonEmpty(strings.TrimSpace(cfg.SessionSpec.Runtime.RunMode), strings.TrimSpace(cfg.Mode)),
			PermissionProfile: strings.TrimSpace(cfg.SessionSpec.Runtime.PermissionProfile),
			SessionPolicy:     strings.TrimSpace(cfg.SessionSpec.Runtime.SessionPolicy),
			ModelProfile:      strings.TrimSpace(cfg.SessionSpec.Runtime.ModelProfile),
		},
		Origin: OriginRequest{Preset: strings.TrimSpace(cfg.SessionSpec.Origin.Preset)},
	}, true
}

func ResolvedSessionSpecFromConfig(cfg kernelsession.SessionConfig) (ResolvedSessionSpec, bool, error) {
	if cfg.ResolvedSessionSpec == nil {
		return ResolvedSessionSpec{}, false, nil
	}
	var policy permissions.CompiledPolicy
	if len(cfg.ResolvedSessionSpec.Runtime.PermissionPolicy) > 0 {
		if err := json.Unmarshal(cfg.ResolvedSessionSpec.Runtime.PermissionPolicy, &policy); err != nil {
			return ResolvedSessionSpec{}, false, fmt.Errorf("unmarshal compiled permission policy: %w", err)
		}
	}
	return ResolvedSessionSpec{
		Goal:      strings.TrimSpace(cfg.Goal),
		Workspace: ResolvedWorkspace{Trust: strings.TrimSpace(cfg.ResolvedSessionSpec.Workspace.Trust)},
		Intent: ResolvedIntent{
			CollaborationMode: strings.TrimSpace(cfg.ResolvedSessionSpec.Intent.CollaborationMode),
			PromptPack: promptpacks.Pack{
				ID:     strings.TrimSpace(cfg.ResolvedSessionSpec.Intent.PromptPack.ID),
				Source: strings.TrimSpace(cfg.ResolvedSessionSpec.Intent.PromptPack.Source),
			},
			CapabilityCeiling: capabilityValues(cfg.ResolvedSessionSpec.Intent.CapabilityCeiling),
		},
		Runtime: ResolvedRuntime{
			RunMode:           strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.RunMode),
			PermissionProfile: strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.PermissionProfile),
			PermissionPolicy:  policy,
			SessionPolicyName: strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.SessionPolicyName),
			SessionPolicy: SessionPolicyConfig{
				MaxSteps:             cfg.ResolvedSessionSpec.Runtime.SessionPolicy.MaxSteps,
				MaxTokens:            cfg.ResolvedSessionSpec.Runtime.SessionPolicy.MaxTokens,
				Timeout:              cfg.ResolvedSessionSpec.Runtime.SessionPolicy.Timeout,
				AutoCompactThreshold: cfg.ResolvedSessionSpec.Runtime.SessionPolicy.AutoCompactThreshold,
				AsyncTaskLimit:       cfg.ResolvedSessionSpec.Runtime.SessionPolicy.AsyncTaskLimit,
			},
			ModelProfile: strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.ModelProfile),
			Model: ModelProfile{
				Provider:        strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.Provider),
				ModelConfig:     cfg.ResolvedSessionSpec.Runtime.ModelConfig,
				ReasoningEffort: strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.ReasoningEffort),
				Verbosity:       strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.Verbosity),
				RouterLane:      strings.TrimSpace(cfg.ResolvedSessionSpec.Runtime.RouterLane),
			},
		},
		Prompt: ResolvedPrompt{
			BasePackID:                 strings.TrimSpace(cfg.ResolvedSessionSpec.Prompt.BasePackID),
			TrustedAugmentationIDs:     append([]string(nil), cfg.ResolvedSessionSpec.Prompt.TrustedAugmentationIDs...),
			TrustedAugmentationDigests: append([]string(nil), cfg.ResolvedSessionSpec.Prompt.TrustedAugmentationDigests...),
			RenderedPromptVersion:      strings.TrimSpace(cfg.ResolvedSessionSpec.Prompt.RenderedPromptVersion),
			SnapshotRef:                strings.TrimSpace(cfg.ResolvedSessionSpec.Prompt.SnapshotRef),
		},
		Origin: ResolvedOrigin{Preset: strings.TrimSpace(cfg.ResolvedSessionSpec.Origin.Preset)},
	}, true, nil
}

func PromptSnapshotFromConfig(cfg kernelsession.SessionConfig) (*PromptSnapshot, bool) {
	if cfg.PromptSnapshot == nil {
		return nil, false
	}
	layers := make([]ResolvedPromptLayer, 0, len(cfg.PromptSnapshot.Layers))
	for _, layer := range cfg.PromptSnapshot.Layers {
		layers = append(layers, ResolvedPromptLayer{
			ID:      strings.TrimSpace(layer.ID),
			Source:  strings.TrimSpace(layer.Source),
			Content: layer.Content,
		})
	}
	return &PromptSnapshot{
		Ref:            strings.TrimSpace(cfg.PromptSnapshot.Ref),
		Layers:         layers,
		RenderedPrompt: cfg.PromptSnapshot.RenderedPrompt,
		Version:        strings.TrimSpace(cfg.PromptSnapshot.Version),
	}, true
}

func capabilityStrings(values []collaboration.Capability) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(string(value)))
	}
	return out
}

func capabilityValues(values []string) []collaboration.Capability {
	if len(values) == 0 {
		return nil
	}
	out := make([]collaboration.Capability, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, collaboration.Capability(trimmed))
	}
	return out
}

func cloneMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxNonZero(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
