package gatewayx

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/gateway"
	"github.com/mossagents/moss/kernel/gateway/channel"
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
}

// ServeCLI 以 CLI Channel + Router + Gateway 的组合方式运行。
func ServeCLI(ctx context.Context, cfg ServeConfig, k *kernel.Kernel) error {
	mgr := k.SessionManager()
	routerCfg := cfg.RouterConfig
	if routerCfg.DefaultConfig.SystemPrompt == "" {
		routerCfg.DefaultConfig.SystemPrompt = cfg.SystemPrompt
	}
	router := session.NewRouter(routerCfg, mgr, cfg.SessionStore)

	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "> "
	}
	cli := channel.NewCLI(channel.WithPrompt(prompt))

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
