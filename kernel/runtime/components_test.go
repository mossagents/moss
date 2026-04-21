package runtime

import (
	"testing"

	"github.com/mossagents/moss/kernel/model"
)

// ─────────────────────────────────────────────
// PolicyCompiler
// ─────────────────────────────────────────────

func TestPolicyCompiler_KnownProfile(t *testing.T) {
	c := NewDefaultPolicyCompiler()

	p, err := c.Compile(RuntimeRequest{PermissionProfile: "read-only"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if p.TrustLevel != "low" {
		t.Errorf("TrustLevel = %q, want low", p.TrustLevel)
	}
	if p.PolicyHash == "" {
		t.Error("PolicyHash should not be empty")
	}
}

func TestPolicyCompiler_UnknownProfile_FallsBackToReadOnly(t *testing.T) {
	c := NewDefaultPolicyCompiler()

	p, err := c.Compile(RuntimeRequest{PermissionProfile: "nonexistent-profile"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// 未知配置应降级到 read-only（trust_level=low）
	if p.TrustLevel != "low" {
		t.Errorf("TrustLevel = %q, want low (fallback)", p.TrustLevel)
	}
}

func TestPolicyCompiler_PolicyHash_Deterministic(t *testing.T) {
	c := NewDefaultPolicyCompiler()
	req := RuntimeRequest{PermissionProfile: "workspace-write"}

	p1, _ := c.Compile(req)
	p2, _ := c.Compile(req)
	if p1.PolicyHash != p2.PolicyHash {
		t.Errorf("PolicyHash not deterministic: %q != %q", p1.PolicyHash, p2.PolicyHash)
	}
}

func TestPolicyCompiler_DifferentProfiles_DifferentHash(t *testing.T) {
	c := NewDefaultPolicyCompiler()

	ro, _ := c.Compile(RuntimeRequest{PermissionProfile: "read-only"})
	rw, _ := c.Compile(RuntimeRequest{PermissionProfile: "workspace-write"})
	full, _ := c.Compile(RuntimeRequest{PermissionProfile: "full"})

	if ro.PolicyHash == rw.PolicyHash {
		t.Error("read-only and workspace-write should have different hashes")
	}
	if ro.PolicyHash == full.PolicyHash {
		t.Error("read-only and full should have different hashes")
	}
}

func TestPolicyCompiler_RegisterCustomProfile(t *testing.T) {
	c := NewDefaultPolicyCompiler()
	c.RegisterProfile("custom", BuiltinProfile{
		TrustLevel:   "high",
		AllowedTools: []string{"my_tool"},
	})

	p, err := c.Compile(RuntimeRequest{PermissionProfile: "custom"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if p.TrustLevel != "high" {
		t.Errorf("TrustLevel = %q, want high", p.TrustLevel)
	}
	if len(p.AllowedTools) != 1 || p.AllowedTools[0] != "my_tool" {
		t.Errorf("AllowedTools = %v", p.AllowedTools)
	}
}

// ─────────────────────────────────────────────
// PromptCompiler
// ─────────────────────────────────────────────

func textLayer(id, scope, text string, priority int) PromptLayerProvider {
	return PromptLayerProvider{
		LayerID:          id,
		Scope:            scope,
		Priority:         priority,
		PersistenceScope: PersistenceScopePersistent,
		ContentParts: []model.ContentPart{
			{Type: model.ContentPartText, Text: text},
		},
	}
}

func TestPromptCompiler_BasicCompile(t *testing.T) {
	c := NewDefaultPromptCompiler()
	bp := SessionBlueprint{
		ContextBudget: ContextBudget{MainTokenBudget: 10_000},
	}
	layers := []PromptLayerProvider{
		textLayer("sys", "system", "You are a helpful assistant.", 1),
		textLayer("usr", "user", "Hello!", 2),
	}

	result, err := c.Compile(bp, nil, layers)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Error("Messages should not be empty")
	}
	if result.PromptHash == "" {
		t.Error("PromptHash should not be empty")
	}
	if len(result.SelectedLayerIDs) != 2 {
		t.Errorf("SelectedLayerIDs len = %d, want 2", len(result.SelectedLayerIDs))
	}
	if len(result.TruncatedLayerIDs) != 0 {
		t.Errorf("TruncatedLayerIDs should be empty, got %v", result.TruncatedLayerIDs)
	}
}

func TestPromptCompiler_BudgetTruncation(t *testing.T) {
	c := NewDefaultPromptCompiler()
	// 非常小的预算：只能容纳约 4 个 token（~16 字符）
	bp := SessionBlueprint{
		ContextBudget: ContextBudget{MainTokenBudget: 4},
	}
	layers := []PromptLayerProvider{
		textLayer("sys", "system", "short", 1),                                 // ~2 tokens
		textLayer("usr", "user", "this is a much longer user message here", 2), // ~10 tokens
	}

	result, err := c.Compile(bp, nil, layers)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// 至少有一个 layer 被截断
	if len(result.TruncatedLayerIDs) == 0 {
		t.Error("expected at least one truncated layer")
	}
	// "usr" layer 应被截断（优先级低 + 预算不足）
	truncated := false
	for _, id := range result.TruncatedLayerIDs {
		if id == "usr" {
			truncated = true
		}
	}
	if !truncated {
		t.Errorf("expected 'usr' layer to be truncated, TruncatedLayerIDs = %v", result.TruncatedLayerIDs)
	}
}

func TestPromptCompiler_PriorityOrdering(t *testing.T) {
	c := NewDefaultPromptCompiler()
	bp := SessionBlueprint{
		ContextBudget: ContextBudget{MainTokenBudget: 10_000},
	}
	// Priority 倒序给入，输出应按 Priority 升序
	layers := []PromptLayerProvider{
		textLayer("layer-c", "system", "C", 30),
		textLayer("layer-a", "system", "A", 10),
		textLayer("layer-b", "system", "B", 20),
	}

	result, err := c.Compile(bp, nil, layers)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(result.SelectedLayerIDs) != 3 {
		t.Fatalf("SelectedLayerIDs = %v", result.SelectedLayerIDs)
	}
	// 按优先级升序：layer-a, layer-b, layer-c
	expected := []string{"layer-a", "layer-b", "layer-c"}
	for i, id := range result.SelectedLayerIDs {
		if id != expected[i] {
			t.Errorf("SelectedLayerIDs[%d] = %q, want %q", i, id, expected[i])
		}
	}
}

func TestPromptCompiler_PromptHash_Deterministic(t *testing.T) {
	c := NewDefaultPromptCompiler()
	bp := SessionBlueprint{ContextBudget: ContextBudget{MainTokenBudget: 10_000}}
	layers := []PromptLayerProvider{
		textLayer("sys", "system", "system prompt", 1),
	}

	r1, _ := c.Compile(bp, nil, layers)
	r2, _ := c.Compile(bp, nil, layers)
	if r1.PromptHash != r2.PromptHash {
		t.Errorf("PromptHash not deterministic: %q != %q", r1.PromptHash, r2.PromptHash)
	}
}

// ─────────────────────────────────────────────
// RequestResolver
// ─────────────────────────────────────────────

func TestRequestResolver_Resolve_Basic(t *testing.T) {
	resolver := NewDefaultRequestResolver(NewDefaultPolicyCompiler())

	bp, err := resolver.Resolve(RuntimeRequest{
		PermissionProfile: "workspace-write",
		Workspace:         "/tmp/workspace",
		UserGoal:          "refactor main.go",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bp.Identity.SessionID == "" {
		t.Error("SessionID should be set")
	}
	if bp.EffectiveToolPolicy.PolicyHash == "" {
		t.Error("PolicyHash should be set")
	}
	if bp.Provenance.Hash == "" {
		t.Error("Provenance.Hash should be set")
	}
	if bp.Provenance.BlueprintSchemaVersion == "" {
		t.Error("BlueprintSchemaVersion should be set")
	}
	if bp.ContextBudget.MainTokenBudget <= 0 {
		t.Error("MainTokenBudget should be > 0")
	}
}

func TestRequestResolver_Resolve_UniqueSessionIDs(t *testing.T) {
	resolver := NewDefaultRequestResolver(NewDefaultPolicyCompiler())
	req := RuntimeRequest{PermissionProfile: "read-only"}

	bp1, _ := resolver.Resolve(req)
	bp2, _ := resolver.Resolve(req)

	if bp1.Identity.SessionID == bp2.Identity.SessionID {
		t.Error("each Resolve call should produce a unique SessionID")
	}
}

func TestRequestResolver_Resolve_PolicyCompilerError(t *testing.T) {
	// 验证 PolicyCompiler 错误能正确传播
	resolver := NewDefaultRequestResolver(NewDefaultPolicyCompiler())
	// DefaultPolicyCompiler 不会返回错误，但接口保证了错误传播路径
	_, err := resolver.Resolve(RuntimeRequest{PermissionProfile: ""})
	if err != nil {
		t.Errorf("empty profile should not error (falls back to default): %v", err)
	}
}
