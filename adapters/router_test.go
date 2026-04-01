package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

// fakeLLM 是测试用的 LLM 假实现。
type fakeLLM struct {
	name string
}

func (f *fakeLLM) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return &port.CompletionResponse{
		Message:    port.Message{Role: port.RoleAssistant, Content: "response from " + f.name},
		StopReason: "end_turn",
	}, nil
}

// fakeStreamingLLM 同时实现 LLM 和 StreamingLLM。
type fakeStreamingLLM struct {
	fakeLLM
}

func (f *fakeStreamingLLM) Stream(_ context.Context, _ port.CompletionRequest) (port.StreamIterator, error) {
	return &emptyIterator{}, nil
}

type emptyIterator struct{}

func (emptyIterator) Next() (port.StreamChunk, error) { return port.StreamChunk{}, io.EOF }
func (emptyIterator) Close() error                    { return nil }

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

	rm, err := r.selectModel(&port.TaskRequirement{})
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
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapFunctionCalling},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "text-only"},
		},
		{
			profile: ModelProfile{
				Name: "multimodal", CostTier: 3,
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapImageGeneration, port.CapFunctionCalling},
			},
			llm: &fakeLLM{name: "multimodal"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&port.TaskRequirement{
		Capabilities: []port.ModelCapability{port.CapImageGeneration},
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
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapCodeGeneration},
			},
			llm: &fakeLLM{name: "expensive"},
		},
		{
			profile: ModelProfile{
				Name: "cheap", CostTier: 1,
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapCodeGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "cheap"},
		},
	}
	r := newTestRouter(models, 1)

	rm, err := r.selectModel(&port.TaskRequirement{
		Capabilities: []port.ModelCapability{port.CapTextGeneration},
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
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapReasoning},
			},
			llm: &fakeLLM{name: "expensive"},
		},
		{
			profile: ModelProfile{
				Name: "mid", CostTier: 2,
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapReasoning},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "mid"},
		},
	}
	r := newTestRouter(models, 1)

	rm, err := r.selectModel(&port.TaskRequirement{
		Capabilities: []port.ModelCapability{port.CapTextGeneration},
		MaxCostTier:  2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "mid" {
		t.Errorf("expected 'mid', got %q", rm.profile.Name)
	}
}

func TestSelectModel_NoMatch_ReturnsError(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "text-only", CostTier: 1,
				Capabilities: []port.ModelCapability{port.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "text-only"},
		},
	}
	r := newTestRouter(models, 0)

	_, err := r.selectModel(&port.TaskRequirement{
		Capabilities: []port.ModelCapability{port.CapImageGeneration},
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
				Capabilities: []port.ModelCapability{port.CapTextGeneration},
				IsDefault:    true,
			},
			llm: &fakeLLM{name: "basic"},
		},
		{
			profile: ModelProfile{
				Name: "advanced", CostTier: 2,
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapReasoning, port.CapCodeGeneration},
			},
			llm: &fakeLLM{name: "advanced"},
		},
	}
	r := newTestRouter(models, 0)

	rm, err := r.selectModel(&port.TaskRequirement{
		Capabilities: []port.ModelCapability{port.CapTextGeneration},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rm.profile.Name != "advanced" {
		t.Errorf("expected strongest model 'advanced', got %q", rm.profile.Name)
	}
}

func TestComplete_UsesSelectedModel(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "default", CostTier: 1, IsDefault: true,
				Capabilities: []port.ModelCapability{port.CapTextGeneration},
			},
			llm: &fakeLLM{name: "default"},
		},
		{
			profile: ModelProfile{
				Name: "vision", CostTier: 2,
				Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapImageUnderstanding},
			},
			llm: &fakeLLM{name: "vision"},
		},
	}
	r := newTestRouter(models, 0)

	// 不指定需求 → 默认模型
	resp, err := r.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "response from default" {
		t.Errorf("expected default model response, got %q", resp.Message.Content)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "default" {
		t.Fatalf("expected actual model metadata for default, got %+v", resp.Metadata)
	}

	// 指定需求 → 选择 vision 模型
	resp, err = r.Complete(context.Background(), port.CompletionRequest{
		Config: port.ModelConfig{
			Requirements: &port.TaskRequirement{
				Capabilities: []port.ModelCapability{port.CapImageUnderstanding},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "response from vision" {
		t.Errorf("expected vision model response, got %q", resp.Message.Content)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "vision" {
		t.Fatalf("expected actual model metadata for vision, got %+v", resp.Metadata)
	}
}

func TestStream_NonStreamingModel_ReturnsError(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "no-stream", CostTier: 1, IsDefault: true,
				Capabilities: []port.ModelCapability{port.CapTextGeneration},
			},
			llm: &fakeLLM{name: "no-stream"},
		},
	}
	r := newTestRouter(models, 0)

	_, err := r.Stream(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error for non-streaming model")
	}
}

func TestStream_StreamingModel_Works(t *testing.T) {
	models := []routedModel{
		{
			profile: ModelProfile{
				Name: "streamer", CostTier: 1, IsDefault: true,
				Capabilities: []port.ModelCapability{port.CapTextGeneration},
			},
			llm: &fakeStreamingLLM{fakeLLM{name: "streamer"}},
		},
	}
	r := newTestRouter(models, 0)

	iter, err := r.Stream(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	provider, ok := iter.(port.MetadataStreamIterator)
	if !ok {
		t.Fatal("expected metadata stream iterator")
	}
	if meta := provider.Metadata(); meta.ActualModel != "streamer" {
		t.Fatalf("expected actual model metadata 'streamer', got %+v", meta)
	}
}

func TestAttachModelMetadata_DirectLLMCallErrorCopiesInsteadOfMutating(t *testing.T) {
	original := &port.LLMCallError{
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
	var callErr *port.LLMCallError
	if !errors.As(got, &callErr) {
		t.Fatalf("expected LLMCallError, got %T", got)
	}
	if callErr.Metadata.ActualModel != "primary" {
		t.Fatalf("actual model = %q, want primary", callErr.Metadata.ActualModel)
	}
}

func TestAttachModelMetadata_WrappedLLMCallErrorPreservesWrapperAndFlags(t *testing.T) {
	inner := &port.LLMCallError{
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
	var callErr *port.LLMCallError
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
		Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapImageGeneration},
	}
	if !p.HasCapability(port.CapTextGeneration) {
		t.Error("expected HasCapability(text_generation) = true")
	}
	if p.HasCapability(port.CapReasoning) {
		t.Error("expected HasCapability(reasoning) = false")
	}
}

func TestHasAllCapabilities(t *testing.T) {
	p := ModelProfile{
		Capabilities: []port.ModelCapability{port.CapTextGeneration, port.CapImageGeneration, port.CapFunctionCalling},
	}
	if !p.HasAllCapabilities([]port.ModelCapability{port.CapTextGeneration, port.CapImageGeneration}) {
		t.Error("expected HasAllCapabilities([text, image]) = true")
	}
	if p.HasAllCapabilities([]port.ModelCapability{port.CapTextGeneration, port.CapReasoning}) {
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
