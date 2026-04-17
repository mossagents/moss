package harness

import "context"

// FeaturePhase controls when a feature is installed relative to the runtime
// assembly flow.
type FeaturePhase string

const (
	// FeaturePhaseConfigure is the default phase for kernel options and
	// capability wiring that should happen before runtime setup.
	FeaturePhaseConfigure FeaturePhase = "configure"
	// FeaturePhaseRuntime is reserved for runtime assembly features such as
	// harness.RuntimeSetup.
	FeaturePhaseRuntime FeaturePhase = "runtime"
	// FeaturePhasePostRuntime runs after runtime setup and is intended for
	// post-runtime augmentations such as policy or patch hooks.
	FeaturePhasePostRuntime FeaturePhase = "post-runtime"
)

// FeatureMetadata provides optional governance hints for harness installation.
// Unannotated features default to FeaturePhaseConfigure with no dependencies.
type FeatureMetadata struct {
	Key      string
	Phase    FeaturePhase
	Requires []string
}

// FeatureWithMetadata is implemented by features that participate in governed
// installation planning.
type FeatureWithMetadata interface {
	Metadata() FeatureMetadata
}

// Feature is a composable capability unit that adds tools, hooks,
// system prompt sections, or other configuration to a Harness.
//
// Features are installed under metadata governance. Official features can
// declare phases and dependencies; unannotated features keep configure-phase
// semantics and preserve input order within their phase.
type Feature interface {
	Name() string
	Install(ctx context.Context, h *Harness) error
}

// Uninstaller is an optional interface that features can implement to support
// rollback when a later feature in the same Install batch fails.
type Uninstaller interface {
	Uninstall(ctx context.Context, h *Harness) error
}

// FeatureFunc adapts a plain function into a Feature.
type FeatureFunc struct {
	FeatureName   string
	MetadataValue FeatureMetadata
	InstallFunc   func(ctx context.Context, h *Harness) error
}

func (f FeatureFunc) Name() string                                  { return f.FeatureName }
func (f FeatureFunc) Metadata() FeatureMetadata                     { return f.MetadataValue }
func (f FeatureFunc) Install(ctx context.Context, h *Harness) error { return f.InstallFunc(ctx, h) }
