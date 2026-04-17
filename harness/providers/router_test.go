package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/model"
)

// fakeLLM 是测试用的 LLM 假实现。
type fakeLLM struct {
	name string
}

func (f *fakeLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return model.ResponseToSeq(&model.CompletionResponse{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("response from " + f.name)}},
		StopReason: "end_turn",
	})
}

// newTestRouter 创建测试用 ModelRouter，直接注入 fakeLLM。
func newTestRouter(models []routedModel, defaultIdx int) *ModelRouter {
	r := &ModelRouter{models: models}
	if defaultIdx >= 0 && defaultIdx < len(models) {
		m := models[defaultIdx]
		r.defaultModel = &m
	}
	return r
}

func TestSelectModel_NoRequirements_ReturnsDefault(t *testing.T) {
	models := []routedModel{
		{profile: ModelProfile{Name: "cheap", CostTier: 1}, llm: &fakeLLM{name: "cheap"}},
		{profile: ModelProfile{Name: "expensive", CostTier: 3, IsDefault: true}, llm: &fakeLLM{name: "expensive"}},
	}
	r := newTestRouter(models, 1)

	rm, err := r.selectModel(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "expensive" {
		t.Errorf("expected default model 'expensive', got %q", rm.profile.Name)
	}
}

func TestOrderedCandidates_NoRequirements_DefaultFirstThenFileOrder(t *testing.T) {
	models := []routedModel{
		{profile: ModelProfile{Name: "cheap", CostTier: 1}, llm: &fakeLLM{name: "cheap"}},
		{profile: ModelProfile{Name: "default", CostTier: 2, IsDefault: true}, llm: &fakeLLM{name: "default"}},
		{profile: ModelProfile{Name: "strong", CostTier: 3}, llm: &fakeLLM{name: "strong"}},
	}
	r := newTestRouter(models, 1)

	candidates, err := r.orderedCandidates(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(candidates), 3; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if candidates[0].profile.Name != "default" || candidates[1].profile.Name != "cheap" || candidates[2].profile.Name != "strong" {
		t.Fatalf("unexpected candidate order: %q, %q, %q", candidates[0].profile.Name, candidates[1].profile.Name, candidates[2].profile.Name)
	}
}

func TestSelectModel_EmptyCapabilities_ReturnsDefault(t *testing.T) {
	models := []routedModel{
		{profile: ModelProfile{Name: "default", CostTier: 1, IsDefault: true}, llm: &fakeLLM{name: "default"}},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "default" {
		t.Errorf("expected 'default', got %q", rm.profile.Name)
	}
}

func TestSelectModel_MatchCapability(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "text-only", CostTier: 1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapFunctionCalling},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "text-only"},
		},
		{
			profile: ModelProfile{
				Name: "multimodal", CostTier: 3,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapImageGeneration, model.CapFunctionCalling},
			},
			llm: &fakeLLM{name: "multimodal"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapImageGeneration},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "multimodal" {
		t.Errorf("expected 'multimodal', got %q", rm.profile.Name)
	}
}

func TestSelectModel_PreferCheap(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "expensive", CostTier: 3,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapCodeGeneration},
			},
			llm: &fakeLLM{name: "expensive"},
		},
		{
			profile: ModelProfile{
				Name: "cheap", CostTier: 1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapCodeGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "cheap"},
		},
	}
	r := newTestRouter(models, 1)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapTextGeneration},
		PreferCheap:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "cheap" {
		t.Errorf("expected 'cheap', got %q", rm.profile.Name)
	}
}

func TestSelectModel_MaxCostTier(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "expensive", CostTier: 3,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapReasoning},
			},
			llm: &fakeLLM{name: "expensive"},
		},
		{
			profile: ModelProfile{
				Name: "mid", CostTier: 2,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapReasoning},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "mid"},
		},
	}
	r := newTestRouter(models, 1)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapTextGeneration},
		MaxCostTier:  2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "mid" {
		t.Errorf("expected 'mid', got %q", rm.profile.Name)
	}
}

func TestSelectModel_ReasoningLanePrefersReasoningModel(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name:         "cheap",
				CostTier:     1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "cheap"},
		},
		{
			profile: ModelProfile{
				Name:         "reasoner",
				CostTier:     3,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapReasoning},
			},
			llm: &fakeLLM{name: "reasoner"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapTextGeneration},
		Lane:         "reasoning",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "reasoner" {
		t.Fatalf("expected reasoner, got %q", rm.profile.Name)
	}
}

func TestSelectModel_ToolHeavyLanePrefersFunctionCallingModel(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name:         "default",
				CostTier:     1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "default"},
		},
		{
			profile: ModelProfile{
				Name:         "tooler",
				CostTier:     2,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapFunctionCalling, model.CapLongContext},
			},
			llm: &fakeLLM{name: "tooler"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapTextGeneration},
		Lane:         "tool-heavy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "tooler" {
		t.Fatalf("expected tooler, got %q", rm.profile.Name)
	}
}

func TestSelectModel_CheapLaneWithoutCapabilitiesStillPrefersCheapModel(t *testing.T) {
	models := []routedModel{
		{profile: ModelProfile{Name: "expensive", CostTier: 3, IsDefault: true}, llm: &fakeLLM{name: "expensive"}},
		{profile: ModelProfile{Name: "cheap", CostTier: 1}, llm: &fakeLLM{name: "cheap"}},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{
		Lane:        "cheap",
		PreferCheap: true,
		MaxCostTier: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "cheap" {
		t.Fatalf("expected cheap, got %q", rm.profile.Name)
	}
}

func TestSelectModel_NoMatch_ReturnsError(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "text-only", CostTier: 1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "text-only"},
		},
	}
	r := newTestRouter(models, 0)

	_, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapImageGeneration},
	})
	if err == nil {
		t.Fatal("expected error for unmatched capability, got nil")
	}
	if !contains(err.Error(), "no model satisfies task requirements") {
		t.Errorf("error message should mention no matching model, got: %v", err)
	}
}

func TestSelectModel_PreferStrongest_WhenNotCheap(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "basic", CostTier: 1,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "basic"},
		},
		{
			profile: ModelProfile{
				Name: "advanced", CostTier: 2,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapReasoning, model.CapCodeGeneration},
			},
			llm: &fakeLLM{name: "advanced"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&model.TaskRequirement{
		Capabilities: []model.ModelCapability{model.CapTextGeneration},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "advanced" {
		t.Errorf("expected strongest model 'advanced', got %q", rm.profile.Name)
	}
}

func TestGenerateContent_UsesSelectedModel(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "default", CostTier: 1, IsDefault: true,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
			},
			llm: &fakeLLM{name: "default"},
		},
		{
			profile: ModelProfile{
				Name: "vision", CostTier: 2,
				Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapImageUnderstanding},
			},
			llm: &fakeLLM{name: "vision"},
		},
	}
	r := newTestRouter(models, 0)

	// 不指定需求 → 默认模型
	resp, err := model.Complete(context.Background(), r, model.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := model.ContentPartsToPlainText(resp.Message.ContentParts); got != "response from default" {
		t.Errorf("expected default model response, got %q", got)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "default" {
		t.Fatalf("expected actual model metadata for default, got %+v", resp.Metadata)
	}

	// 指定需求 → 选择 vision 模型
	resp, err = model.Complete(context.Background(), r, model.CompletionRequest{
		Config: model.ModelConfig{
			Requirements: &model.TaskRequirement{
				Capabilities: []model.ModelCapability{model.CapImageUnderstanding},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := model.ContentPartsToPlainText(resp.Message.ContentParts); got != "response from vision" {
		t.Errorf("expected vision model response, got %q", got)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "vision" {
		t.Fatalf("expected actual model metadata for vision, got %+v", resp.Metadata)
	}
}

func TestGenerateContent_StreamsMetadata(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "streamer", CostTier: 1, IsDefault: true,
				Capabilities: []model.ModelCapability{model.CapTextGeneration},
			},
			llm: &fakeLLM{name: "streamer"},
		},
	}
	r := newTestRouter(models, 0)

	var meta *model.LLMCallMetadata
	for chunk, err := range r.GenerateContent(context.Background(), model.CompletionRequest{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if chunk.Metadata != nil {
			meta = chunk.Metadata
		}
	}
	if meta == nil || meta.ActualModel != "streamer" {
		t.Fatalf("expected actual model metadata 'streamer', got %+v", meta)
	}
}

func TestAttachModelMetadata_DirectLLMCallErrorCopiesInsteadOfMutating(t *testing.T) {
	original := &model.LLMCallError{
		Err:          io.ErrUnexpectedEOF,
		Retryable:    true,
		FallbackSafe: true,
	}

	got := attachModelMetadata(original, "primary")
	if got == original {
		t.Fatal("expected attachModelMetadata to return a copied error")
	}
	if strings.TrimSpace(original.Metadata.ActualModel) != "" {
		t.Fatalf("original error metadata was mutated: %+v", original.Metadata)
	}
	var callErr *model.LLMCallError
	if !errors.As(got, &callErr) {
		t.Fatalf("expected LLMCallError, got %T", got)
	}
	if callErr.Metadata.ActualModel != "primary" {
		t.Fatalf("actual model = %q, want primary", callErr.Metadata.ActualModel)
	}
}

func TestAttachModelMetadata_WrappedLLMCallErrorPreservesWrapperAndFlags(t *testing.T) {
	inner := &model.LLMCallError{
		Err:          fmt.Errorf("provider rejected request"),
		Retryable:    false,
		FallbackSafe: false,
	}
	wrapped := fmt.Errorf("transport failure: %w", inner)

	got := attachModelMetadata(wrapped, "primary")
	if !strings.Contains(got.Error(), "transport failure") {
		t.Fatalf("expected wrapper message to be preserved, got %v", got)
	}
	if strings.TrimSpace(inner.Metadata.ActualModel) != "" {
		t.Fatalf("wrapped inner error metadata was mutated: %+v", inner.Metadata)
	}
	var callErr *model.LLMCallError
	if !errors.As(got, &callErr) {
		t.Fatalf("expected LLMCallError, got %T", got)
	}
	if callErr.Retryable || callErr.FallbackSafe {
		t.Fatalf("expected original flags to be preserved, got retryable=%v fallbackSafe=%v", callErr.Retryable, callErr.FallbackSafe)
	}
	if callErr.Metadata.ActualModel != "primary" {
		t.Fatalf("actual model = %q, want primary", callErr.Metadata.ActualModel)
	}
}

func TestHasCapability(t *testing.T) {
	p := ModelProfile{
		Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapImageGeneration},
	}
	if !p.HasCapability(model.CapTextGeneration) {
		t.Error("expected HasCapability(text_generation) = true")
	}
	if p.HasCapability(model.CapReasoning) {
		t.Error("expected HasCapability(reasoning) = false")
	}
}

func TestHasAllCapabilities(t *testing.T) {
	p := ModelProfile{
		Capabilities: []model.ModelCapability{model.CapTextGeneration, model.CapImageGeneration, model.CapFunctionCalling},
	}
	if !p.HasAllCapabilities([]model.ModelCapability{model.CapTextGeneration, model.CapImageGeneration}) {
		t.Error("expected HasAllCapabilities([text, image]) = true")
	}
	if p.HasAllCapabilities([]model.ModelCapability{model.CapTextGeneration, model.CapReasoning}) {
		t.Error("expected HasAllCapabilities([text, reasoning]) = false")
	}
}

func TestNewModelRouterFromFile(t *testing.T) {
	yaml := `models:
  - name: test-model
    provider: openai
    model: gpt-4o-mini
    cost_tier: 1
    capabilities:
      - text_generation
      - function_calling
    is_default: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	r, err := NewModelRouterFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Models()) != 1 {
		t.Errorf("expected 1 model, got %d", len(r.Models()))
	}
	if r.DefaultModel() != "test-model" {
		t.Errorf("expected default 'test-model', got %q", r.DefaultModel())
	}
}

func TestNewModelRouter_EmptyProfiles_Error(t *testing.T) {
	_, err := NewModelRouter(nil)
	if err == nil {
		t.Fatal("expected error for empty profiles")
	}
}

func TestNewModelRouter_NoExplicitDefault_UsesFirst(t *testing.T) {
	profiles := []ModelProfile{
		{Name: "first", Provider: "openai", Model: "gpt-4o-mini", CostTier: 1},
		{Name: "second", Provider: "openai", Model: "gpt-4o", CostTier: 2},
	}
	r, err := NewModelRouter(profiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.DefaultModel() != "first" {
		t.Errorf("expected default 'first', got %q", r.DefaultModel())
	}
}

func TestNewModelRouterFromConfig(t *testing.T) {
	cfg := &appconfig.Config{Models: []appconfig.ModelConfig{
		{Provider: "openai", Model: "gpt-4o-mini", Default: true},
		{Provider: "openai-responses", Model: "gpt-5"},
	}}
	r, err := NewModelRouterFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewModelRouterFromConfig: %v", err)
	}
	if len(r.Models()) != 2 {
		t.Fatalf("expected 2 models, got %d", len(r.Models()))
	}
	if r.DefaultModel() != "gpt-4o-mini" {
		t.Fatalf("default model = %q, want gpt-4o-mini", r.DefaultModel())
	}
	models := r.Models()
	if !models[1].HasCapability(model.CapReasoning) {
		t.Fatalf("expected gpt-5 synthesized profile to include reasoning: %+v", models[1])
	}
	if models[0].CostTier != 1 {
		t.Fatalf("expected gpt-4o-mini cost tier 1, got %d", models[0].CostTier)
	}
}

func TestModels_ReturnsReadOnlyCopy(t *testing.T) {
	models := []routedModel{
		{profile: ModelProfile{Name: "a"}, llm: &fakeLLM{}},
		{profile: ModelProfile{Name: "b"}, llm: &fakeLLM{}},
	}
	r := newTestRouter(models, 0)

	result := r.Models()
	if len(result) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result))
	}
	// 修改返回值不影响内部状态
	result[0].Name = "modified"
	if r.models[0].profile.Name == "modified" {
		t.Error("Models() should return a copy, not internal reference")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
