package appkit

import (
	"context"

	"github.com/mossagents/moss/appkit/runtime"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/io"
)

// RuntimeResolution captures resolved runtime inputs shared by CLI/TUI entrypoints.
type RuntimeResolution struct {
	Profile            runtime.ResolvedProfile
	ConfigInstructions string
	ModelInstructions  string
	FeatureFlags       RuntimeFeatureFlags
}

// RuntimeFeatureFlags captures runtime middleware capability switches.
type RuntimeFeatureFlags struct {
	EnableSummarize bool
	EnableRAG       bool
	PromptAssembly  string
	PromptVersion   string
}

func resolveRuntimeFeatureFlags(flags *AppFlags) RuntimeFeatureFlags {
	if flags == nil {
		return RuntimeFeatureFlags{}
	}
	return RuntimeFeatureFlags{
		EnableSummarize: flags.EnableSummarize,
		EnableRAG:       flags.EnableRAG,
		PromptAssembly:  flags.PromptAssembly,
		PromptVersion:   flags.PromptVersion,
	}
}

// RuntimeBuilder centralizes profile/instruction resolution and kernel wiring.
type RuntimeBuilder struct{}

func NewRuntimeBuilder() RuntimeBuilder {
	return RuntimeBuilder{}
}

// Resolve applies profile resolution and prompt-instruction layer resolution.
func (RuntimeBuilder) Resolve(flags *AppFlags) (RuntimeResolution, error) {
	resolvedProfile, err := runtime.ResolveProfileForWorkspace(runtime.ProfileResolveOptions{
		Workspace:        flags.Workspace,
		RequestedProfile: flags.Profile,
		Trust:            flags.Trust,
	})
	if err != nil {
		return RuntimeResolution{}, err
	}

	flags.Profile = resolvedProfile.Name
	flags.Trust = resolvedProfile.Trust

	configInstructions, modelInstructions, err := config.ResolvePromptInstructionLayers(flags.Workspace, flags.Trust)
	if err != nil {
		return RuntimeResolution{}, err
	}

	return RuntimeResolution{
		Profile:            resolvedProfile,
		ConfigInstructions: configInstructions,
		ModelInstructions:  modelInstructions,
		FeatureFlags:       resolveRuntimeFeatureFlags(flags),
	}, nil
}

// BuildKernel resolves runtime inputs then builds a kernel with the resolved flags.
func (b RuntimeBuilder) BuildKernel(ctx context.Context, flags *AppFlags, io intr.UserIO, extraOpts ...kernel.Option) (*kernel.Kernel, RuntimeResolution, error) {
	resolution, err := b.Resolve(flags)
	if err != nil {
		return nil, RuntimeResolution{}, err
	}
	k, err := BuildKernel(ctx, flags, io, extraOpts...)
	if err != nil {
		return nil, RuntimeResolution{}, err
	}
	return k, resolution, nil
}
