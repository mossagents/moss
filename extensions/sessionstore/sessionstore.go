package sessionstore

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// WithStore 将 SessionStore 作为标准扩展接入 Kernel。
func WithStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		kernel.Extensions(k).SetSessionStore(store)
	}
}
