package providers

import (
	"fmt"
	"strings"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/model"
)

// NewModelRouterFromConfig 从应用配置里的 models 列表合成 router。
// 配置文件未显式声明 models 时，会复用 config 包对顶层 provider/model 的归一化结果。
func NewModelRouterFromConfig(cfg *appconfig.Config) (*ModelRouter, error) {
	profiles := modelProfilesFromConfig(cfg)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("model router: no configured models available")
	}
	return NewModelRouter(profiles)
}

func modelProfilesFromConfig(cfg *appconfig.Config) []ModelProfile {
	if cfg == nil || len(cfg.Models) == 0 {
		return nil
	}
	profiles := make([]ModelProfile, 0, len(cfg.Models))
	seenNames := make(map[string]int, len(cfg.Models))
	for index, configured := range cfg.Models {
		identity := appconfig.NormalizeProviderIdentity(configured.Provider, configured.Name)
		profile := ModelProfile{
			Name:         uniqueProfileName(configured, identity, index, seenNames),
			Provider:     identity.Provider,
			Model:        strings.TrimSpace(configured.Model),
			APIKey:       strings.TrimSpace(configured.APIKey),
			BaseURL:      strings.TrimSpace(configured.BaseURL),
			CostTier:     inferModelCostTier(identity.Provider, configured.Model),
			Capabilities: inferModelCapabilities(identity.Provider, configured.Model),
			IsDefault:    configured.Default,
		}
		profiles = append(profiles, profile)
	}
	if len(profiles) > 0 {
		hasDefault := false
		for _, profile := range profiles {
			if profile.IsDefault {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			profiles[0].IsDefault = true
		}
	}
	return profiles
}

func uniqueProfileName(configured appconfig.ModelConfig, identity appconfig.ProviderIdentity, index int, seen map[string]int) string {
	name := strings.TrimSpace(configured.Name)
	modelName := strings.TrimSpace(configured.Model)
	if modelName != "" && (name == "" || strings.EqualFold(name, identity.Provider) || strings.EqualFold(name, identity.Name)) {
		name = modelName
	}
	if name == "" {
		name = identity.Name
	}
	if name == "" {
		name = fmt.Sprintf("model-%d", index+1)
	}
	if seen[name] == 0 {
		seen[name] = 1
		return name
	}
	seen[name]++
	return fmt.Sprintf("%s-%d", name, seen[name])
}

func inferModelCapabilities(provider, modelName string) []model.ModelCapability {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	caps := []model.ModelCapability{model.CapTextGeneration, model.CapCodeGeneration}
	switch provider {
	case appconfig.APITypeOpenAICompletions, appconfig.APITypeOpenAIResponses, appconfig.APITypeClaude, appconfig.APITypeGemini:
		caps = append(caps, model.CapFunctionCalling)
	}
	switch provider {
	case appconfig.APITypeOpenAICompletions, appconfig.APITypeOpenAIResponses, appconfig.APITypeClaude, appconfig.APITypeGemini:
		caps = append(caps, model.CapImageUnderstanding)
	}
	if looksLikeReasoningModel(provider, modelName) {
		caps = append(caps, model.CapReasoning)
	}
	if looksLikeLongContextModel(provider, modelName) {
		caps = append(caps, model.CapLongContext)
	}
	return caps
}

func looksLikeReasoningModel(provider, modelName string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if provider == appconfig.APITypeClaude || provider == appconfig.APITypeGemini {
		return true
	}
	for _, marker := range []string{"gpt-5", "o1", "o3", "o4", "reason", "thinking", "r1", "sonnet", "opus"} {
		if strings.Contains(modelName, marker) {
			return true
		}
	}
	return false
}

func looksLikeLongContextModel(provider, modelName string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if provider == appconfig.APITypeClaude || provider == appconfig.APITypeGemini {
		return true
	}
	for _, marker := range []string{"gpt-4.1", "gpt-5", "128k", "200k", "long", "extended"} {
		if strings.Contains(modelName, marker) {
			return true
		}
	}
	return false
}

func inferModelCostTier(provider, modelName string) int {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	for _, marker := range []string{"nano", "mini", "haiku", "flash", "lite", "small"} {
		if strings.Contains(modelName, marker) {
			return 1
		}
	}
	for _, marker := range []string{"opus", "ultra", "max", "o1", "o3", "o4", "pro", "gpt-5"} {
		if strings.Contains(modelName, marker) {
			return 3
		}
	}
	if provider == appconfig.APITypeClaude || provider == appconfig.APITypeGemini {
		return 2
	}
	return 2
}
