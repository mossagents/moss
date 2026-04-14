package memory

import "context"

// ContextInjectConfig controls how a memory provider renders prompt-visible
// context for a session.
type ContextInjectConfig struct {
	SessionID string
	Query     string
	EpisodicN int
	SemanticK int
	Threshold float64
	MaxChars  int
}

// ContextInjector is the kernel-level contract for injecting prompt-visible
// memory or knowledge context ahead of model calls.
type ContextInjector interface {
	InjectContext(ctx context.Context, cfg ContextInjectConfig) (string, error)
}
