package capability_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/internal/runtime/capability"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/skill"
)

// ── RegisterProgressiveSkillTools ────────────────────────────────────────────

func TestRegisterProgressiveSkillTools_NilKernel(t *testing.T) {
	err := capability.RegisterProgressiveSkillTools(nil)
	if err == nil {
		t.Fatal("expected error for nil kernel")
	}
}

func TestRegisterProgressiveSkillTools_Success(t *testing.T) {
	k := kernel.New()
	err := capability.RegisterProgressiveSkillTools(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("list_skills"); !ok {
		t.Error("expected list_skills to be registered")
	}
	if _, ok := k.ToolRegistry().Get("activate_skill"); !ok {
		t.Error("expected activate_skill to be registered")
	}
}

func TestRegisterProgressiveSkillTools_Idempotent(t *testing.T) {
	k := kernel.New()
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatalf("second call should be no-op: %v", err)
	}
}

// ── list_skills tool ──────────────────────────────────────────────────────────

func TestListSkillsTool_EmptyManifests(t *testing.T) {
	k := kernel.New()
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, ok := k.ToolRegistry().Get("list_skills")
	if !ok {
		t.Fatal("list_skills not found")
	}
	result, err := entry.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out []any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty list, got %d items", len(out))
	}
}

func TestListSkillsTool_WithManifests(t *testing.T) {
	k := kernel.New()
	capability.SetSkillManifests(k, []skill.Manifest{
		{Name: "skill-a", Description: "Skill A"},
		{Name: "skill-b", Description: "Skill B"},
	})
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("list_skills")
	result, err := entry.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
	if out[0]["name"] != "skill-a" {
		t.Errorf("unexpected first skill name: %v", out[0]["name"])
	}
	loaded, _ := out[0]["loaded"].(bool)
	if loaded {
		t.Error("expected skill-a to be unloaded initially")
	}
}

// ── activate_skill tool ───────────────────────────────────────────────────────

func TestActivateSkillTool_EmptyName(t *testing.T) {
	k := kernel.New()
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("activate_skill")
	_, err := entry.Execute(context.Background(), json.RawMessage(`{"name":""}`))
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestActivateSkillTool_MissingName(t *testing.T) {
	k := kernel.New()
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("activate_skill")
	_, err := entry.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestActivateSkillTool_NotFoundInManifests(t *testing.T) {
	k := kernel.New()
	capability.SetSkillManifests(k, []skill.Manifest{
		{Name: "existing-skill"},
	})
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("activate_skill")
	_, err := entry.Execute(context.Background(), json.RawMessage(`{"name":"unknown-skill"}`))
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestActivateSkillTool_CycleDetection(t *testing.T) {
	k := kernel.New()
	// skill-a → skill-b → skill-a (cycle)
	capability.SetSkillManifests(k, []skill.Manifest{
		{Name: "skill-a", DependsOn: []string{"skill-b"}, Source: "/nonexistent/skill-a/SKILL.md"},
		{Name: "skill-b", DependsOn: []string{"skill-a"}, Source: "/nonexistent/skill-b/SKILL.md"},
	})
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("activate_skill")
	_, err := entry.Execute(context.Background(), json.RawMessage(`{"name":"skill-a"}`))
	if err == nil {
		t.Fatal("expected error for cyclic dependency")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected 'cycle' in error, got: %v", err)
	}
}

func TestActivateSkillTool_DependencyNotFound(t *testing.T) {
	k := kernel.New()
	capability.SetSkillManifests(k, []skill.Manifest{
		{Name: "skill-a", DependsOn: []string{"missing-dep"}, Source: "/nonexistent/SKILL.md"},
	})
	if err := capability.RegisterProgressiveSkillTools(k); err != nil {
		t.Fatal(err)
	}
	entry, _ := k.ToolRegistry().Get("activate_skill")
	_, err := entry.Execute(context.Background(), json.RawMessage(`{"name":"skill-a"}`))
	if err == nil {
		t.Fatal("expected error when dependency not found")
	}
}

// ── CapabilityDeps ────────────────────────────────────────────────────────────

func TestCapabilityDeps_DoesNotPanic(t *testing.T) {
	k := kernel.New()
	deps := capability.CapabilityDeps(k)
	if deps.Kernel == nil {
		t.Error("expected Kernel in deps")
	}
	if deps.ToolRegistry == nil {
		t.Error("expected ToolRegistry in deps")
	}
}

// ── Prompt generation ─────────────────────────────────────────────────────────

func TestPromptGeneration_NoManager(t *testing.T) {
	k := kernel.New()
	capability.Ensure(k)
	// Manager is set but progressive is false; with empty manifests, prompt additions should be empty
	prompt := k.Prompts().Extend(k, "base")
	// No panic; prompt may just contain "base"
	_ = prompt
}

func TestPromptGeneration_NonProgressive_NoManifests(t *testing.T) {
	k := kernel.New()
	capability.Ensure(k)
	prompt := k.Prompts().Extend(k, "")
	// manager exists, progressive=false, no manifests → SystemPromptAdditions() returns ""
	_ = prompt
}

func TestPromptGeneration_ProgressiveWithUnloadedManifests(t *testing.T) {
	k := kernel.New()
	capability.SetSkillManifests(k, []skill.Manifest{
		{Name: "skill-alpha", Description: "Alpha skill"},
		{Name: "skill-beta", Description: "Beta skill"},
	})
	capability.EnableProgressiveSkills(k)

	prompt := k.Prompts().Extend(k, "")
	if !strings.Contains(prompt, "skill-alpha") {
		t.Errorf("expected skill-alpha in progressive prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "activate_skill") {
		t.Errorf("expected activate_skill hint in prompt, got: %s", prompt)
	}
}

func TestPromptGeneration_ProgressiveNoManifests(t *testing.T) {
	k := kernel.New()
	// progressive=true but no manifests → hint omitted
	capability.EnableProgressiveSkills(k)
	prompt := k.Prompts().Extend(k, "")
	if strings.Contains(prompt, "activate_skill") {
		t.Errorf("expected no activate_skill hint when no manifests, got: %s", prompt)
	}
}
