package appkit

import (
	"context"
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// ServeConfig 配置 Gateway Serve 模式。
type ServeConfig struct {
	// Prompt 是 CLI 终端的输入提示符。
	Prompt string

	// SystemPrompt 为 Gateway 创建 Session 时注入的系统提示词。
	SystemPrompt string

	// SessionStore 为 Gateway 路由提供可选的会话持久化支持。
	SessionStore session.SessionStore

	// RouterConfig 会话路由配置。
	RouterConfig session.RouterConfig

	// OnError 可选的错误回调。
	OnError func(error)

	// DeliveryDir 为 gateway 可靠投递配置持久化目录（可选）。
	DeliveryDir string

	// RouteScope 会话路由键粒度（main/per-peer/per-channel-peer/per-account-channel-peer）。
	RouteScope string
}

// Serve 以 Gateway 模式运行：CLI Channel → Router → Kernel.Run → 回复。
//
// 这是 REPL 的 Gateway 替代方案，基于 P0 引入的 Channel + Router 抽象。
// 与 REPL 不同，Serve 通过 Channel 接口驱动，可扩展到 WebSocket 等通道。
func Serve(ctx context.Context, cfg ServeConfig, k *kernel.Kernel) error {
	return runtime.ServeCLI(ctx, runtime.ServeConfig{
		Prompt:       cfg.Prompt,
		SystemPrompt: cfg.SystemPrompt,
		SessionStore: cfg.SessionStore,
		RouterConfig: cfg.RouterConfig,
		OnError:      cfg.OnError,
		DeliveryDir:  cfg.DeliveryDir,
		RouteScope:   cfg.RouteScope,
	}, k)
}
