package harness

import "context"

// Feature is a composable capability unit that adds tools, hooks,
// system prompt sections, or other configuration to a Harness.
//
// Features are installed in order; a feature may depend on capabilities
// provided by a previously installed feature.
type Feature interface {
	Name() string
	Install(ctx context.Context, h *Harness) error
}

// FeatureFunc adapts a plain function into a Feature.
type FeatureFunc struct {
	FeatureName string
	InstallFunc func(ctx context.Context, h *Harness) error
}

func (f FeatureFunc) Name() string                              { return f.FeatureName }
func (f FeatureFunc) Install(ctx context.Context, h *Harness) error { return f.InstallFunc(ctx, h) }
