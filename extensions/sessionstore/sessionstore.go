package sessionstore

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// Deprecated: use runtime.WithKernelSessionStore.
func WithStore(store session.SessionStore) kernel.Option {
	return runtime.WithKernelSessionStore(store)
}
