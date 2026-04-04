package tui

import (
	"context"
	"fmt"
	"strings"

	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
)

func buildRuntimeKernel(cfg Config, wCfg WelcomeConfig, bridge *BridgeIO) (*kernel.Kernel, context.Context, context.CancelFunc, error) {
	provider := strings.ToLower(configpkg.NormalizeProviderIdentity("", wCfg.Provider, wCfg.ProviderName).EffectiveAPIType())
	k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, cfg.ApprovalMode, cfg.Profile, provider, wCfg.Model, cfg.APIKey, cfg.BaseURL, bridge)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize kernel: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := k.Boot(ctx); err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("failed to boot kernel: %w", err)
	}
	if cfg.AfterBoot != nil {
		if err := cfg.AfterBoot(ctx, k, bridge); err != nil {
			cancel()
			return nil, nil, nil, fmt.Errorf("failed to initialize runtime: %w", err)
		}
	}
	return k, ctx, cancel, nil
}
