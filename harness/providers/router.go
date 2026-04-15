package providers

import (
	"context"
	"errors"
	"fmt"
	config "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/model"
	"gopkg.in/yaml.v3"
	"iter"
	"os"
	"sort"
	"strings"
)

// ModelProfile 描述一个模型的能力画像和连接信息。
// 通常从 YAML 配置文件加载。
type ModelProfile struct {
	// Name 模型配置的唯一标识名，用于日志和调试。
	Name string `yaml:"name"`

	// Provider LLM 协议类型: "openai-completions" / "openai-responses" / "claude" / "gemini"。
	Provider string `yaml:"provider"`

	// Model 具体的模型名称，如 "gpt-4o", "claude-sonnet-4-20250514"
	Model string `yaml:"model"`

	// APIKey API 密钥，为空则使用环境变量。
	APIKey string `yaml:"api_key,omitempty"`

	// BaseURL 自定义 API 端点，为空则使用默认值。
	BaseURL string `yaml:"base_url,omitempty"`

	// CostTier 成本等级：1=低, 2=中, 3=高
	CostTier int `yaml:"cost_tier"`

	// Capabilities 模型具备的能力列表。
	Capabilities []model.ModelCapability `yaml:"capabilities"`

	// MaxTokens 最大输出 token 数。
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// IsDefault 是否为默认模型（无需求时使用）。
	IsDefault bool `yaml:"is_default,omitempty"`
}

// HasCapability 检查模型是否具备指定能力。
func (p *ModelProfile) HasCapability(cap model.ModelCapability) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// HasAllCapabilities 检查模型是否具备所有指定能力。
func (p *ModelProfile) HasAllCapabilities(caps []model.ModelCapability) bool {
	for _, cap := range caps {
		if !p.HasCapability(cap) {
			return false
		}
	}
	return true
}

// routedModel 将配置画像与实际 LLM 实例关联。
type routedModel struct {
	profile ModelProfile
	llm     model.LLM
}

// ModelRouter 根据任务需求动态选择最优模型。
// 实现 model.LLM 接口，可直接传入 kernel.WithLLM()。
type ModelRouter struct {
	models       []routedModel
	defaultModel *routedModel
}

// RouterConfig 是 ModelRouter 的 YAML 配置结构。
type RouterConfig struct {
	Models []ModelProfile `yaml:"models"`
}

// NewModelRouter 从模型画像列表创建 ModelRouter。
// 每个画像会通过 BuildLLM 自动创建对应的 LLM 实例。
// 至少需要配置一个模型，且必须指定一个 is_default 模型。
func NewModelRouter(profiles []ModelProfile) (*ModelRouter, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("model router: at least one model profile is required")
	}

	r := &ModelRouter{}
	for _, p := range profiles {
		identity := config.NormalizeProviderIdentity(p.Provider, p.Name)
		p.Provider = identity.Provider
		p.Name = identity.Name
		llm, err := BuildLLM(identity.EffectiveAPIType(), p.Model, p.APIKey, p.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("model router: build %q: %w", p.Name, err)
		}
		rm := routedModel{profile: p, llm: llm}
		r.models = append(r.models, rm)
		if p.IsDefault {
			copied := rm
			r.defaultModel = &copied
		}
	}

	if r.defaultModel == nil {
		// 未显式指定默认模型时使用第一个
		first := r.models[0]
		r.defaultModel = &first
	}

	return r, nil
}

// NewModelRouterFromFile 从 YAML 配置文件加载 ModelRouter。
//
// 配置文件格式:
//
//	models:
//	  - name: gpt-4o
//	    provider: openai-completions
//	    model: gpt-4o
//	    cost_tier: 3
//	    capabilities: [text_generation, code_generation, image_understanding, function_calling, reasoning]
//	    is_default: true
//	  - name: gpt-4o-mini
//	    provider: openai-completions
//	    model: gpt-4o-mini
//	    cost_tier: 1
//	    capabilities: [text_generation, code_generation, function_calling]
func NewModelRouterFromFile(path string) (*ModelRouter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("model router: read config: %w", err)
	}

	var cfg RouterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("model router: parse config: %w", err)
	}

	return NewModelRouter(cfg.Models)
}

// GenerateContent 根据请求中的 TaskRequirement 选择最优模型并调用。
// 若未指定需求，使用默认模型。
func (r *ModelRouter) GenerateContent(ctx context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	rm, err := r.selectModel(req.Config.Requirements)
	if err != nil {
		return func(yield func(model.StreamChunk, error) bool) {
			yield(model.StreamChunk{}, err)
		}
	}
	return func(yield func(model.StreamChunk, error) bool) {
		for chunk, err := range rm.llm.GenerateContent(ctx, req) {
			if err != nil {
				yield(model.StreamChunk{}, attachModelMetadata(err, rm.profile.Name))
				return
			}
			// Attach model metadata to final chunk.
			if chunk.Done {
				if chunk.Metadata == nil {
					chunk.Metadata = &model.LLMCallMetadata{ActualModel: rm.profile.Name}
				} else if strings.TrimSpace(chunk.Metadata.ActualModel) == "" {
					chunk.Metadata.ActualModel = rm.profile.Name
				}
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// Models 返回已注册的所有模型画像（只读副本）。
func (r *ModelRouter) Models() []ModelProfile {
	out := make([]ModelProfile, len(r.models))
	for i, rm := range r.models {
		out[i] = rm.profile
	}
	return out
}

// DefaultModel 返回默认模型的名称。
func (r *ModelRouter) DefaultModel() string {
	if r.defaultModel != nil {
		return r.defaultModel.profile.Name
	}
	return ""
}

// selectModel 根据任务需求选择最优模型。
func (r *ModelRouter) selectModel(req *model.TaskRequirement) (*routedModel, error) {
	candidates, err := r.orderedCandidates(req)
	if err != nil {
		return nil, err
	}
	selected := candidates[0]
	return &selected, nil
}

func (r *ModelRouter) orderedCandidates(req *model.TaskRequirement) ([]routedModel, error) {
	if req == nil {
		return r.defaultOrderedCandidates(), nil
	}

	var candidates []routedModel
	for _, rm := range r.models {
		if len(req.Capabilities) > 0 && !rm.profile.HasAllCapabilities(req.Capabilities) {
			continue
		}
		if req.MaxCostTier > 0 && rm.profile.CostTier > req.MaxCostTier {
			continue
		}
		candidates = append(candidates, rm)
	}

	if len(candidates) == 0 {
		return nil, r.noModelError(req)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := routeScore(candidates[i].profile, req)
		right := routeScore(candidates[j].profile, req)
		if left != right {
			return left > right
		}
		if req.PreferCheap || strings.TrimSpace(req.Lane) == "cheap" || strings.TrimSpace(req.Lane) == "background-task" {
			if candidates[i].profile.CostTier != candidates[j].profile.CostTier {
				return candidates[i].profile.CostTier < candidates[j].profile.CostTier
			}
		}
		if len(candidates[i].profile.Capabilities) != len(candidates[j].profile.Capabilities) {
			return len(candidates[i].profile.Capabilities) > len(candidates[j].profile.Capabilities)
		}
		return candidates[i].profile.Name < candidates[j].profile.Name
	})
	return candidates, nil
}

func routeScore(profile ModelProfile, req *model.TaskRequirement) int {
	score := 0
	if req == nil {
		return score
	}
	switch strings.TrimSpace(req.Lane) {
	case "cheap":
		score += 100 - profile.CostTier*10
	case "background-task":
		score += 80 - profile.CostTier*10
	case "reasoning":
		if profile.HasCapability(model.CapReasoning) {
			score += 120
		}
	case "tool-heavy":
		if profile.HasCapability(model.CapFunctionCalling) {
			score += 100
		}
		if profile.HasCapability(model.CapLongContext) {
			score += 20
		}
	}
	if req.PreferCheap {
		score += 40 - profile.CostTier*10
	}
	score += len(profile.Capabilities)
	return score
}

func (r *ModelRouter) defaultOrderedCandidates() []routedModel {
	if len(r.models) == 0 {
		return nil
	}
	out := make([]routedModel, 0, len(r.models))
	seen := map[string]struct{}{}
	if r.defaultModel != nil {
		out = append(out, *r.defaultModel)
		seen[r.defaultModel.profile.Name] = struct{}{}
	}
	for _, rm := range r.models {
		if _, ok := seen[rm.profile.Name]; ok {
			continue
		}
		out = append(out, rm)
		seen[rm.profile.Name] = struct{}{}
	}
	return out
}

// noModelError 生成详细的无模型可用错误信息。
func (r *ModelRouter) noModelError(req *model.TaskRequirement) error {
	var needed []string
	for _, cap := range req.Capabilities {
		needed = append(needed, string(cap))
	}

	var available []string
	for _, rm := range r.models {
		var caps []string
		for _, c := range rm.profile.Capabilities {
			caps = append(caps, string(c))
		}
		available = append(available, fmt.Sprintf(
			"  - %s (cost_tier=%d, capabilities=[%s])",
			rm.profile.Name,
			rm.profile.CostTier,
			strings.Join(caps, ", "),
		))
	}

	msg := fmt.Sprintf(
		"model router: no model satisfies task requirements\n"+
			"  required capabilities: [%s]\n",
		strings.Join(needed, ", "),
	)
	if req.MaxCostTier > 0 {
		msg += fmt.Sprintf("  max cost tier: %d\n", req.MaxCostTier)
	}
	msg += "  已注册模型:\n" + strings.Join(available, "\n")

	return fmt.Errorf("%s", msg)
}

func attachModelMetadata(err error, modelName string) error {
	if err == nil {
		return nil
	}
	if callErr, ok := err.(*model.LLMCallError); ok {
		if strings.TrimSpace(callErr.Metadata.ActualModel) != "" {
			return err
		}
		merged := *callErr
		merged.Metadata.ActualModel = modelName
		return &merged
	}
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		if strings.TrimSpace(callErr.Metadata.ActualModel) != "" {
			return err
		}
		return &model.LLMCallError{
			Err:          err,
			Retryable:    callErr.Retryable,
			FallbackSafe: callErr.FallbackSafe,
			Metadata:     model.LLMCallMetadata{ActualModel: modelName},
		}
	}
	return &model.LLMCallError{
		Err:          err,
		Retryable:    true,
		FallbackSafe: true,
		Metadata:     model.LLMCallMetadata{ActualModel: modelName},
	}
}
