package appkit

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/gateway"
	"github.com/mossagi/moss/kernel/gateway/channel"
	"github.com/mossagi/moss/kernel/session"
)

// ServeConfig 配置 Gateway Serve 模式。
type ServeConfig struct {
	// Prompt 是 CLI 终端的输入提示符。
	Prompt string

	// SystemPrompt 为 Gateway 创建 Session 时注入的系统提示词。
	SystemPrompt string

	// RouterConfig 会话路由配置。
	RouterConfig session.RouterConfig

	// OnError 可选的错误回调。
	OnError func(error)
}

// Serve 以 Gateway 模式运行：CLI Channel → Router → Kernel.Run → 回复。
//
// 这是 REPL 的 Gateway 替代方案，基于 P0 引入的 Channel + Router 抽象。
// 与 REPL 不同，Serve 通过 Channel 接口驱动，可扩展到 WebSocket 等通道。
func Serve(ctx context.Context, cfg ServeConfig, k *kernel.Kernel) error {
	// 构建 Router
	mgr := k.SessionManager()
	store := k.SessionStore()
	routerCfg := cfg.RouterConfig
	if routerCfg.DefaultConfig.SystemPrompt == "" {
		routerCfg.DefaultConfig.SystemPrompt = cfg.SystemPrompt
	}
	router := session.NewRouter(routerCfg, mgr, store)

	// 构建 CLI Channel
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "> "
	}
	cli := channel.NewCLI(channel.WithPrompt(prompt))

	// 构建 Gateway
	onError := cfg.OnError
	if onError == nil {
		onError = func(err error) {
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n\n", err)
		}
	}
	gw := gateway.New(k, router,
		gateway.WithSystemPrompt(cfg.SystemPrompt),
		gateway.WithOnError(onError),
	)
	gw.AddChannel(cli)

	return gw.Serve(ctx)
}
