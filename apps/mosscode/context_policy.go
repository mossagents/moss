package main

import (
	"strings"

	"github.com/mossagents/moss/harness/appkit"
	runtimectx "github.com/mossagents/moss/harness/runtime/runctx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

type contextPolicy struct {
	KeepRecent    int
	TriggerTokens int
	PromptBudget  int
	StartupBudget int
}

func configureContextPolicy(k *kernel.Kernel, flags *appkit.AppFlags) error {
	policy := resolveContextPolicy(flags)
	return k.Apply(runtimectx.ConfigureContext(
		runtimectx.WithKeepRecent(policy.KeepRecent),
		runtimectx.WithContextTriggerTokens(policy.TriggerTokens),
		runtimectx.WithContextPromptBudget(policy.PromptBudget),
		runtimectx.WithContextStartupBudget(policy.StartupBudget),
	))
}

func applyContextPolicy(cfg session.SessionConfig, flags *appkit.AppFlags) session.SessionConfig {
	policy := resolveContextPolicy(flags)
	if cfg.ModelConfig.ContextWindow <= 0 {
		cfg.ModelConfig.ContextWindow = policy.PromptBudget
	}
	if cfg.ModelConfig.AutoCompactTokenLimit <= 0 {
		cfg.ModelConfig.AutoCompactTokenLimit = policy.TriggerTokens
	}
	return cfg
}

func resolveContextPolicy(flags *appkit.AppFlags) contextPolicy {
	window := inferContextWindow(flags)
	if window <= 0 {
		window = 32000
	}
	trigger := int(float64(window) * 0.75)
	startup := window / 10
	if startup < 3000 {
		startup = 3000
	}
	if startup > 16000 {
		startup = 16000
	}
	keepRecent := 48
	if window <= 64000 {
		keepRecent = 40
	}
	return contextPolicy{
		KeepRecent:    keepRecent,
		TriggerTokens: trigger,
		PromptBudget:  window,
		StartupBudget: startup,
	}
}

func inferContextWindow(flags *appkit.AppFlags) int {
	if flags == nil {
		return 32000
	}
	modelName := strings.ToLower(strings.TrimSpace(flags.Model))
	provider := strings.ToLower(strings.TrimSpace(flags.EffectiveAPIType()))
	if modelName == "" {
		switch provider {
		case "claude", "anthropic", "gemini", "google", "openai-responses":
			return 128000
		default:
			return 64000
		}
	}
	switch {
	case strings.Contains(modelName, "gpt-5"):
		return 200000
	case strings.Contains(modelName, "gpt-4.1"), strings.Contains(modelName, "o3"), strings.Contains(modelName, "o4"):
		return 128000
	case strings.Contains(modelName, "gpt-4o"):
		return 128000
	case strings.Contains(modelName, "claude-sonnet-4"), strings.Contains(modelName, "claude-opus-4"), strings.Contains(modelName, "claude-3.7"):
		return 200000
	case strings.Contains(modelName, "claude"):
		return 128000
	case strings.Contains(modelName, "gemini-2.5-pro"):
		return 200000
	case strings.Contains(modelName, "gemini-2.5"), strings.Contains(modelName, "gemini-2.0"), strings.Contains(modelName, "gemini-1.5"):
		return 128000
	case strings.Contains(modelName, "mini"), strings.Contains(modelName, "flash"):
		return 64000
	default:
		return 64000
	}
}
