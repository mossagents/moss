package tui

import (
	"context"
	"fmt"
	"strings"

	configpkg "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func buildRuntimeKernel(cfg Config, wCfg WelcomeConfig, bridge *bridgeIO) (*kernel.Kernel, context.Context, context.CancelFunc, error) {
	provider := strings.ToLower(configpkg.NormalizeProviderIdentity(wCfg.Provider, wCfg.ProviderName).EffectiveAPIType())
	k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, cfg.ApprovalMode, provider, wCfg.Model, cfg.APIKey, cfg.BaseURL, bridge)
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

// blueprintToSessionConfig 将 SessionBlueprint 的关键字段转换成 legacy session.SessionConfig，
// 供 TUI resume 路径在 kernel.NewSession 时使用。
func blueprintToSessionConfig(bp kruntime.SessionBlueprint) session.SessionConfig {
	trustLevel := bp.EffectiveToolPolicy.TrustLevel
	if trustLevel == "" {
		trustLevel = "workspace-write"
	}
	return session.SessionConfig{
		Goal:       bp.Identity.AgentName,
		TrustLevel: trustLevel,
		MaxSteps:   bp.SessionBudget.MaxSteps,
		MaxTokens:  bp.ContextBudget.MainTokenBudget,
		Mode:       bp.PromptPlan.PromptPackID,
		ModelConfig: model.ModelConfig{
			Model:     bp.ModelConfig.ModelID,
			MaxTokens: bp.ContextBudget.MainTokenBudget,
		},
		Metadata: map[string]any{
			"blueprint_session_id":     bp.Identity.SessionID,
			"blueprint_hash":           bp.Provenance.Hash,
			"blueprint_schema_version": bp.Provenance.BlueprintSchemaVersion,
		},
	}
}
