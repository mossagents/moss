package sessionspec

import (
	"fmt"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/collaboration"
	"github.com/mossagents/moss/harness/runtime/permissions"
	"github.com/mossagents/moss/harness/runtime/presets"
	"github.com/mossagents/moss/harness/runtime/promptpacks"
	"github.com/mossagents/moss/kernel/model"
)

type SessionSpec struct {
	Goal      string
	Workspace WorkspaceRequest
	Intent    IntentRequest
	Runtime   RuntimeRequest
	Origin    OriginRequest
}

type WorkspaceRequest struct {
	Trust string
}

type IntentRequest struct {
	CollaborationMode string
	PromptPack        string
}

type RuntimeRequest struct {
	RunMode           string
	PermissionProfile string
	SessionPolicy     string
	ModelProfile      string
}

type OriginRequest struct {
	Preset string
}

type Defaults struct {
	Preset            string
	PromptPack        string
	CollaborationMode string
	PermissionProfile string
	SessionPolicy     string
	ModelProfile      string
}

type SessionPolicyConfig struct {
	MaxSteps             int
	MaxTokens            int
	Timeout              time.Duration
	AutoCompactThreshold int
	AsyncTaskLimit       int
}

type ModelProfile struct {
	Provider        string
	ModelConfig     model.ModelConfig
	ReasoningEffort string
	Verbosity       string
	RouterLane      string
}

type ResolvedPromptLayer struct {
	ID      string
	Source  string
	Content string
}

type PromptSnapshot struct {
	Ref            string
	Layers         []ResolvedPromptLayer
	RenderedPrompt string
	Version        string
}

type ResolvedSessionSpec struct {
	Goal      string
	Workspace ResolvedWorkspace
	Intent    ResolvedIntent
	Runtime   ResolvedRuntime
	Prompt    ResolvedPrompt
	Origin    ResolvedOrigin
}

type ResolvedWorkspace struct {
	Trust string
}

type ResolvedIntent struct {
	CollaborationMode string
	PromptPack        promptpacks.Pack
	CapabilityCeiling []collaboration.Capability
}

type ResolvedRuntime struct {
	RunMode           string
	PermissionProfile string
	PermissionPolicy  permissions.CompiledPolicy
	SessionPolicyName string
	SessionPolicy     SessionPolicyConfig
	ModelProfile      string
	Model             ModelProfile
}

type ResolvedPrompt struct {
	BasePackID                 string
	TrustedAugmentationIDs     []string
	TrustedAugmentationDigests []string
	RenderedPromptVersion      string
	SnapshotRef                string
}

type ResolvedOrigin struct {
	Preset string
}

type Registries struct {
	PromptPacks        map[string]promptpacks.Pack
	Presets            map[string]presets.Preset
	PermissionProfiles map[string]permissions.Profile
	SessionPolicies    map[string]SessionPolicyConfig
	ModelProfiles      map[string]ModelProfile
}

type ResolveInput struct {
	WorkspaceTrust    string
	DefaultRunMode    string
	GlobalDefaults    Defaults
	WorkspaceDefaults Defaults
	Registries        Registries
}

func Resolve(spec SessionSpec, input ResolveInput) (ResolvedSessionSpec, error) {
	trust, err := resolveTrust(spec.Workspace.Trust, input.WorkspaceTrust)
	if err != nil {
		return ResolvedSessionSpec{}, err
	}
	runMode := strings.TrimSpace(spec.Runtime.RunMode)
	if runMode == "" {
		runMode = strings.TrimSpace(input.DefaultRunMode)
	}
	if err := validateRunMode(runMode); err != nil {
		return ResolvedSessionSpec{}, err
	}
	working := spec
	if explicitPreset := strings.TrimSpace(working.Origin.Preset); explicitPreset != "" {
		if err := applyPreset(&working, input.Registries.Presets, explicitPreset); err != nil {
			return ResolvedSessionSpec{}, err
		}
	}
	if appconfig.ProjectAssetsAllowed(trust) {
		if err := applyDefaults(&working, input.Registries.Presets, input.WorkspaceDefaults); err != nil {
			return ResolvedSessionSpec{}, err
		}
	}
	if err := applyDefaults(&working, input.Registries.Presets, input.GlobalDefaults); err != nil {
		return ResolvedSessionSpec{}, err
	}
	if strings.TrimSpace(working.Intent.PromptPack) == "" {
		return ResolvedSessionSpec{}, fmt.Errorf("prompt_pack is required")
	}
	rawMode := strings.TrimSpace(working.Intent.CollaborationMode)
	if rawMode == "" {
		return ResolvedSessionSpec{}, fmt.Errorf("collaboration_mode is required")
	}
	mode := collaboration.NormalizeMode(rawMode)
	if err := mode.Validate(); err != nil {
		return ResolvedSessionSpec{}, err
	}
	pack, err := promptpacks.Resolve(input.Registries.PromptPacks, working.Intent.PromptPack)
	if err != nil {
		return ResolvedSessionSpec{}, err
	}
	policy, err := permissions.Resolve(input.Registries.PermissionProfiles, working.Runtime.PermissionProfile, trust)
	if err != nil {
		return ResolvedSessionSpec{}, err
	}
	sessionPolicy, ok := input.Registries.SessionPolicies[strings.TrimSpace(working.Runtime.SessionPolicy)]
	if !ok {
		return ResolvedSessionSpec{}, fmt.Errorf("unknown session policy %q", strings.TrimSpace(working.Runtime.SessionPolicy))
	}
	modelProfileName := strings.TrimSpace(working.Runtime.ModelProfile)
	modelProfile, ok := input.Registries.ModelProfiles[modelProfileName]
	if !ok {
		return ResolvedSessionSpec{}, fmt.Errorf("unknown model profile %q", modelProfileName)
	}
	resolved := ResolvedSessionSpec{
		Goal:      strings.TrimSpace(working.Goal),
		Workspace: ResolvedWorkspace{Trust: trust},
		Intent: ResolvedIntent{
			CollaborationMode: string(mode),
			PromptPack:        pack,
			CapabilityCeiling: collaboration.CeilingForMode(mode).Slice(),
		},
		Runtime: ResolvedRuntime{
			RunMode:           runMode,
			PermissionProfile: strings.TrimSpace(working.Runtime.PermissionProfile),
			PermissionPolicy:  policy,
			SessionPolicyName: strings.TrimSpace(working.Runtime.SessionPolicy),
			SessionPolicy:     sessionPolicy,
			ModelProfile:      modelProfileName,
			Model:             modelProfile,
		},
		Prompt: ResolvedPrompt{BasePackID: pack.ID},
		Origin: ResolvedOrigin{Preset: strings.TrimSpace(working.Origin.Preset)},
	}
	return resolved, nil
}

func applyDefaults(spec *SessionSpec, registry map[string]presets.Preset, defaults Defaults) error {
	if spec == nil {
		return nil
	}
	if strings.TrimSpace(spec.Origin.Preset) == "" && strings.TrimSpace(defaults.Preset) != "" {
		if err := applyPreset(spec, registry, defaults.Preset); err != nil {
			return err
		}
	}
	if strings.TrimSpace(spec.Intent.PromptPack) == "" {
		spec.Intent.PromptPack = strings.TrimSpace(defaults.PromptPack)
	}
	if strings.TrimSpace(spec.Intent.CollaborationMode) == "" {
		spec.Intent.CollaborationMode = strings.TrimSpace(defaults.CollaborationMode)
	}
	if strings.TrimSpace(spec.Runtime.PermissionProfile) == "" {
		spec.Runtime.PermissionProfile = strings.TrimSpace(defaults.PermissionProfile)
	}
	if strings.TrimSpace(spec.Runtime.SessionPolicy) == "" {
		spec.Runtime.SessionPolicy = strings.TrimSpace(defaults.SessionPolicy)
	}
	if strings.TrimSpace(spec.Runtime.ModelProfile) == "" {
		spec.Runtime.ModelProfile = strings.TrimSpace(defaults.ModelProfile)
	}
	return nil
}

func applyPreset(spec *SessionSpec, registry map[string]presets.Preset, presetName string) error {
	preset, err := presets.Resolve(registry, presetName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.Origin.Preset) == "" {
		spec.Origin.Preset = preset.ID
	}
	if strings.TrimSpace(spec.Intent.PromptPack) == "" {
		spec.Intent.PromptPack = preset.PromptPack
	}
	if strings.TrimSpace(spec.Intent.CollaborationMode) == "" {
		spec.Intent.CollaborationMode = preset.CollaborationMode
	}
	if strings.TrimSpace(spec.Runtime.PermissionProfile) == "" {
		spec.Runtime.PermissionProfile = preset.PermissionProfile
	}
	if strings.TrimSpace(spec.Runtime.SessionPolicy) == "" {
		spec.Runtime.SessionPolicy = preset.SessionPolicy
	}
	if strings.TrimSpace(spec.Runtime.ModelProfile) == "" {
		spec.Runtime.ModelProfile = preset.ModelProfile
	}
	return nil
}

func resolveTrust(explicit, fallback string) (string, error) {
	trust := strings.TrimSpace(explicit)
	if trust == "" {
		trust = strings.TrimSpace(fallback)
	}
	trust = strings.ToLower(trust)
	switch trust {
	case appconfig.TrustTrusted, appconfig.TrustRestricted:
		return trust, nil
	default:
		return "", fmt.Errorf("workspace_trust is required and must be trusted or restricted")
	}
}

func validateRunMode(runMode string) error {
	switch strings.ToLower(strings.TrimSpace(runMode)) {
	case "interactive", "oneshot", "batch", "background":
		return nil
	default:
		return fmt.Errorf("run_mode is required and must be interactive, oneshot, batch, or background")
	}
}
