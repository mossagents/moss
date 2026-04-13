package runtime

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// WithKernelSessionStore attaches a SessionStore via kernel-managed persistence hooks.
func WithKernelSessionStore(store session.SessionStore) kernel.Option {
	return kernel.WithPersistentSessionStore(store)
}
