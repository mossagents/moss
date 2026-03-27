package runtime

import (
	"github.com/mossagents/moss/extensions/compactx"
	"github.com/mossagents/moss/extensions/contextx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

type ContextOption = contextx.Option

func WithContextSessionStore(store session.SessionStore) kernel.Option {
	return contextx.WithSessionStore(store)
}

func WithContextSessionManager(manager session.Manager) kernel.Option {
	return contextx.WithSessionManager(manager)
}

func ConfigureContext(opts ...ContextOption) kernel.Option {
	return contextx.Configure(opts...)
}

func WithOffloadSessionStore(store session.SessionStore) kernel.Option {
	return compactx.WithSessionStore(store)
}

func RegisterOffloadTools(reg tool.Registry, store session.SessionStore, manager session.Manager) error {
	return compactx.RegisterTools(reg, store, manager)
}
