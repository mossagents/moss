package runtimecapability_test

import (
	"testing"

	"github.com/mossagents/moss/capability"
	"github.com/mossagents/moss/internal/runtimecapability"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/skill"
)

func TestEnsure_NilKernel(t *testing.T) {
	st := runtimecapability.Ensure(nil)
	if st != nil {
		t.Fatal("expected nil for nil kernel")
	}
}

func TestEnsure_ValidKernel(t *testing.T) {
	k := kernel.New()
	st := runtimecapability.Ensure(k)
	if st == nil {
		t.Fatal("expected non-nil State")
	}
	// Same instance on repeated calls
	st2 := runtimecapability.Ensure(k)
	if st != st2 {
		t.Fatal("expected same State instance")
	}
}

func TestLookup_NotFound(t *testing.T) {
	k := kernel.New()
	_, ok := runtimecapability.Lookup(k)
	if ok {
		t.Fatal("expected not found before Ensure")
	}
}

func TestLookup_Found(t *testing.T) {
	k := kernel.New()
	runtimecapability.Ensure(k)
	_, ok := runtimecapability.Lookup(k)
	if !ok {
		t.Fatal("expected to find state after Ensure")
	}
}

func TestLookup_NilKernel(t *testing.T) {
	_, ok := runtimecapability.Lookup(nil)
	if ok {
		t.Fatal("expected false for nil kernel")
	}
}

func TestManager_NilKernel(t *testing.T) {
	m := runtimecapability.Manager(nil)
	if m != nil {
		t.Fatal("expected nil manager for nil kernel")
	}
}

func TestManager_ValidKernel(t *testing.T) {
	k := kernel.New()
	m := runtimecapability.Manager(k)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestLookupManager_NotFound(t *testing.T) {
	k := kernel.New()
	_, ok := runtimecapability.LookupManager(k)
	if ok {
		t.Fatal("expected not found before Ensure")
	}
}

func TestLookupManager_Found(t *testing.T) {
	k := kernel.New()
	runtimecapability.Ensure(k)
	_, ok := runtimecapability.LookupManager(k)
	if !ok {
		t.Fatal("expected to find manager after Ensure")
	}
}

func TestLookupSkillManifests_Empty(t *testing.T) {
	k := kernel.New()
	runtimecapability.Ensure(k)
	manifests := runtimecapability.LookupSkillManifests(k)
	if manifests != nil {
		t.Errorf("expected nil manifests initially, got %v", manifests)
	}
}

func TestLookupSkillManifests_NilKernel(t *testing.T) {
	manifests := runtimecapability.LookupSkillManifests(nil)
	if manifests != nil {
		t.Error("expected nil for nil kernel")
	}
}

func TestSetSkillManifests(t *testing.T) {
	k := kernel.New()
	manifests := []skill.Manifest{
		{Name: "skill-a", Description: "Skill A"},
		{Name: "skill-b", Description: "Skill B"},
	}
	runtimecapability.SetSkillManifests(k, manifests)
	got := runtimecapability.LookupSkillManifests(k)
	if len(got) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(got))
	}
	if got[0].Name != "skill-a" || got[1].Name != "skill-b" {
		t.Errorf("unexpected manifests: %+v", got)
	}
}

func TestSetSkillManifests_IsolatesInput(t *testing.T) {
	k := kernel.New()
	manifests := []skill.Manifest{{Name: "skill-x"}}
	runtimecapability.SetSkillManifests(k, manifests)
	// Mutate original slice
	manifests[0].Name = "mutated"
	got := runtimecapability.LookupSkillManifests(k)
	if got[0].Name == "mutated" {
		t.Error("SetSkillManifests should store a copy")
	}
}

func TestSetManager(t *testing.T) {
	k := kernel.New()
	m := capability.NewManager()
	runtimecapability.SetManager(k, m)
	got, ok := runtimecapability.LookupManager(k)
	if !ok || got == nil {
		t.Fatal("expected manager after SetManager")
	}
}

func TestSetManager_NilKernel(t *testing.T) {
	// Should not panic
	runtimecapability.SetManager(nil, capability.NewManager())
}

func TestEnableProgressiveSkills(t *testing.T) {
	k := kernel.New()
	// Should not panic; coverage for the flag-set path
	runtimecapability.EnableProgressiveSkills(k)
}

func TestEnableProgressiveSkills_NilKernel(t *testing.T) {
	// Should not panic
	runtimecapability.EnableProgressiveSkills(nil)
}
