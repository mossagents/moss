package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
	"gopkg.in/yaml.v3"
)

// ModelProfile 描述一个模型的能力画像和连接信息。
// 通常从 YAML 配置文件加载。
type ModelProfile struct {
	// Name 模型配置的唯一标识名，用于日志和调试。
	Name string `yaml:"name"`

	// APIType API 协议类型: "openai" / "claude" / "anthropic"
	APIType string `yaml:"api_type,omitempty"`

	// Provider 兼容旧配置，等价于 APIType。
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
	Capabilities []port.ModelCapability `yaml:"capabilities"`

	// MaxTokens 最大输出 token 数。
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// IsDefault 是否为默认模型（无需求时使用）。
	IsDefault bool `yaml:"is_default,omitempty"`
}

// HasCapability 检查模型是否具备指定能力。
func (p *ModelProfile) HasCapability(cap port.ModelCapability) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// HasAllCapabilities 检查模型是否具备所有指定能力。
func (p *ModelProfile) HasAllCapabilities(caps []port.ModelCapability) bool {
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
	llm     port.LLM
}

// ModelRouter 根据任务需求动态选择最优模型。
// 实现 port.LLM 和 port.StreamingLLM 接口，可直接传入 kernel.WithLLM()。
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
		apiType := config.NormalizeProviderIdentity(p.APIType, p.Provider, "").EffectiveAPIType()
		llm, err := BuildLLM(apiType, p.Model, p.APIKey, p.BaseURL)
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
//	    api_type: openai
//	    model: gpt-4o
//	    cost_tier: 3
//	    capabilities: [text_generation, code_generation, image_understanding, function_calling, reasoning]
//	    is_default: true
//	  - name: gpt-4o-mini
//	    api_type: openai
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

// Complete 根据请求中的 TaskRequirement 选择最优模型并调用。
// 若未指定需求，使用默认模型。
func (r *ModelRouter) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	rm, err := r.selectModel(req.Config.Requirements)
	if err != nil {
		return nil, err
	}
	resp, err := rm.llm.Complete(ctx, req)
	if err != nil {
		return nil, attachModelMetadata(err, rm.profile.Name)
	}
	if resp.Metadata == nil {
		resp.Metadata = &port.LLMCallMetadata{}
	}
	if strings.TrimSpace(resp.Metadata.ActualModel) == "" {
		resp.Metadata.ActualModel = rm.profile.Name
	}
	return resp, nil
}

// Stream 根据请求中的 TaskRequirement 选择最优模型并以流式调用。
// 若选中的模型不支持 StreamingLLM，返回错误。
func (r *ModelRouter) Stream(ctx context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
	rm, err := r.selectModel(req.Config.Requirements)
	if err != nil {
		return nil, err
	}
	sllm, ok := rm.llm.(port.StreamingLLM)
	if !ok {
		return nil, &port.LLMCallError{
			Err:          fmt.Errorf("model router: selected model %q does not support streaming", rm.profile.Name),
			Retryable:    true,
			FallbackSafe: true,
			Metadata:     port.LLMCallMetadata{ActualModel: rm.profile.Name},
		}
	}
	iter, err := sllm.Stream(ctx, req)
	if err != nil {
		return nil, attachModelMetadata(err, rm.profile.Name)
	}
	return &routerStreamIterator{
		inner: iter,
		metadata: port.LLMCallMetadata{
			ActualModel: rm.profile.Name,
		},
	}, nil
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
func (r *ModelRouter) selectModel(req *port.TaskRequirement) (*routedModel, error) {
	candidates, err := r.orderedCandidates(req)
	if err != nil {
		return nil, err
	}
	selected := candidates[0]
	return &selected, nil
}

func (r *ModelRouter) orderedCandidates(req *port.TaskRequirement) ([]routedModel, error) {
	if req == nil || len(req.Capabilities) == 0 {
		return r.defaultOrderedCandidates(), nil
	}

	var candidates []routedModel
	for _, rm := range r.models {
		if !rm.profile.HasAllCapabilities(req.Capabilities) {
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

	if req.PreferCheap {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].profile.CostTier < candidates[j].profile.CostTier
		})
	} else {
		sort.Slice(candidates, func(i, j int) bool {
			return len(candidates[i].profile.Capabilities) > len(candidates[j].profile.Capabilities)
		})
	}
	return candidates, nil
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
func (r *ModelRouter) noModelError(req *port.TaskRequirement) error {
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
		"model router: 没有模型满足任务需求\n"+
			"  所需能力: [%s]\n",
		strings.Join(needed, ", "),
	)
	if req.MaxCostTier > 0 {
		msg += fmt.Sprintf("  最高成本等级: %d\n", req.MaxCostTier)
	}
	msg += "  已注册模型:\n" + strings.Join(available, "\n")

	return fmt.Errorf("%s", msg)
}

type routerStreamIterator struct {
	inner    port.StreamIterator
	metadata port.LLMCallMetadata
}

func (it *routerStreamIterator) Next() (port.StreamChunk, error) {
	return it.inner.Next()
}

func (it *routerStreamIterator) Close() error {
	return it.inner.Close()
}

func (it *routerStreamIterator) Metadata() port.LLMCallMetadata {
	if provider, ok := it.inner.(port.MetadataStreamIterator); ok {
		meta := provider.Metadata()
		if strings.TrimSpace(meta.ActualModel) == "" {
			meta.ActualModel = it.metadata.ActualModel
		}
		return meta
	}
	return it.metadata
}

func attachModelMetadata(err error, model string) error {
	if err == nil {
		return nil
	}
	if callErr, ok := err.(*port.LLMCallError); ok {
		if strings.TrimSpace(callErr.Metadata.ActualModel) != "" {
			return err
		}
		merged := *callErr
		merged.Metadata.ActualModel = model
		return &merged
	}
	var callErr *port.LLMCallError
	if errors.As(err, &callErr) {
		if strings.TrimSpace(callErr.Metadata.ActualModel) != "" {
			return err
		}
		return &port.LLMCallError{
			Err:          err,
			Retryable:    callErr.Retryable,
			FallbackSafe: callErr.FallbackSafe,
			Metadata:     port.LLMCallMetadata{ActualModel: model},
		}
	}
	return &port.LLMCallError{
		Err:          err,
		Retryable:    true,
		FallbackSafe: true,
		Metadata:     port.LLMCallMetadata{ActualModel: model},
	}
}
