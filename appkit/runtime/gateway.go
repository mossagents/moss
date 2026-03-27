package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagents/moss/gateway"
	"github.com/mossagents/moss/gateway/channel"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

type ServeConfig struct {
	Prompt       string
	SystemPrompt string
	SessionStore session.SessionStore
	RouterConfig session.RouterConfig
	OnError      func(error)
}

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
		onError = func(err error) { fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n\n", err) }
	}
	gw := gateway.New(k, router, gateway.WithSystemPrompt(cfg.SystemPrompt), gateway.WithOnError(onError))
	gw.AddChannel(cli)
	return gw.Serve(ctx)
}
