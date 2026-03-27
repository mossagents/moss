package compactx

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// WithSessionStore 注入 offload 使用的 SessionStore。
// Deprecated: use runtime.WithOffloadSessionStore.
func WithSessionStore(store session.SessionStore) kernel.Option {
	return runtime.WithOffloadSessionStore(store)
}

// RegisterTools 注册上下文压缩工具。
// Deprecated: use runtime.RegisterOffloadTools.
func RegisterTools(reg tool.Registry, store session.SessionStore, manager session.Manager) error {
	return runtime.RegisterOffloadTools(reg, store, manager)
}
