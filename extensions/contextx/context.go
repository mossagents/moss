package contextx

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

type Option = runtime.ContextOption

// WithTriggerDialogCount 设置自动压缩触发阈值（按非 system 消息数）。
// Deprecated: use runtime.WithTriggerDialogCount.
func WithTriggerDialogCount(n int) Option { return runtime.WithTriggerDialogCount(n) }

// WithKeepRecent 设置自动压缩保留的最近对话数。
// Deprecated: use runtime.WithKeepRecent.
func WithKeepRecent(n int) Option { return runtime.WithKeepRecent(n) }

// WithSessionStore 设置上下文快照持久化存储。
// Deprecated: use runtime.WithContextSessionStore.
func WithSessionStore(store session.SessionStore) kernel.Option {
	return runtime.WithContextSessionStore(store)
}

// WithSessionManager 设置会话管理器。
// Deprecated: use runtime.WithContextSessionManager.
func WithSessionManager(manager session.Manager) kernel.Option {
	return runtime.WithContextSessionManager(manager)
}

// Configure 配置 contextx 扩展参数。
// Deprecated: use runtime.ConfigureContext.
func Configure(opts ...Option) kernel.Option { return runtime.ConfigureContext(opts...) }
