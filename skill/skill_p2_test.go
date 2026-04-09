package skill_test

import (
	"context"
	"errors"
	"github.com/mossagents/moss/skill"
	"testing"
)

// ---- version tests -------------------------------------------------------

func TestParseVersion(t *testing.T) {
	cases := []struct {
		input string
		want  [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"v2.0.0", [3]int{2, 0, 0}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"", [3]int{0, 0, 0}},
		{"1.0", [3]int{1, 0, 0}},
	}
	for _, c := range cases {
		got, err := skill.ParseVersion(c.input)
		if err != nil {
			t.Errorf("ParseVersion(%q) error: %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestCompareVersion(t *testing.T) {
	if skill.CompareVersion("1.0.0", "2.0.0") >= 0 {
		t.Error("expected 1.0.0 < 2.0.0")
	}
	if skill.CompareVersion("2.0.0", "1.9.9") <= 0 {
		t.Error("expected 2.0.0 > 1.9.9")
	}
	if skill.CompareVersion("1.2.3", "1.2.3") != 0 {
		t.Error("expected 1.2.3 == 1.2.3")
	}
}

func TestIsVersionInRange(t *testing.T) {
	if !skill.IsVersionInRange("1.5.0", "1.0.0", "2.0.0") {
		t.Error("1.5.0 should be in [1.0.0, 2.0.0]")
	}
	if skill.IsVersionInRange("0.9.0", "1.0.0", "2.0.0") {
		t.Error("0.9.0 should not be in [1.0.0, 2.0.0]")
	}
	if skill.IsVersionInRange("2.1.0", "1.0.0", "2.0.0") {
		t.Error("2.1.0 should not be in [1.0.0, 2.0.0]")
	}
	if !skill.IsVersionInRange("1.0.0", "", "2.0.0") {
		t.Error("1.0.0 should be in [*, 2.0.0]")
	}
}

// ---- topology + dependency tests ----------------------------------------

type stubSkill struct {
	name     string
	version  string
	requires []skill.SkillDep
	deps     []string
}

func (s *stubSkill) Metadata() skill.Metadata {
	return skill.Metadata{Name: s.name, Version: s.version, Requires: s.requires, DependsOn: s.deps}
}
func (s *stubSkill) Init(_ context.Context, _ skill.Deps) error { return nil }
func (s *stubSkill) Shutdown(_ context.Context) error           { return nil }

func TestTopologicalSort_NoDeps(t *testing.T) {
	a := &stubSkill{name: "a"}
	b := &stubSkill{name: "b"}
	sorted, err := skill.TopologicalSort([]skill.Provider{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 2 {
		t.Errorf("expected 2 providers, got %d", len(sorted))
	}
}

func TestTopologicalSort_WithDeps(t *testing.T) {
	a := &stubSkill{name: "a"}
	b := &stubSkill{name: "b", deps: []string{"a"}}
	sorted, err := skill.TopologicalSort([]skill.Provider{b, a})
	if err != nil {
		t.Fatal(err)
	}
	// a must come before b
	order := map[string]int{}
	for i, p := range sorted {
		order[p.Metadata().Name] = i
	}
	if order["a"] >= order["b"] {
		t.Errorf("expected a before b, got order: %v", order)
	}
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	a := &stubSkill{name: "a", deps: []string{"b"}}
	b := &stubSkill{name: "b", deps: []string{"a"}}
	_, err := skill.TopologicalSort([]skill.Provider{a, b})
	if err == nil {
		t.Error("expected cycle error, got nil")
	}
}

func TestValidateDeps_VersionConstraint(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()

	base := &stubSkill{name: "base", version: "1.5.0"}
	if err := mgr.Register(ctx, base, skill.Deps{}); err != nil {
		t.Fatal(err)
	}

	// dependent requires base >=1.0.0 <=2.0.0 — should pass
	dep := &stubSkill{
		name:     "dep",
		version:  "1.0.0",
		requires: []skill.SkillDep{{Name: "base", MinVersion: "1.0.0", MaxVersion: "2.0.0"}},
	}
	if err := mgr.Register(ctx, dep, skill.Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ValidateDeps(dep); err != nil {
		t.Errorf("expected valid deps, got: %v", err)
	}
}

func TestValidateDeps_VersionViolation(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()

	base := &stubSkill{name: "base", version: "0.5.0"}
	_ = mgr.Register(ctx, base, skill.Deps{})

	dep := &stubSkill{
		name:     "dep",
		requires: []skill.SkillDep{{Name: "base", MinVersion: "1.0.0"}},
	}
	_ = mgr.Register(ctx, dep, skill.Deps{})
	err := mgr.ValidateDeps(dep)
	if err == nil {
		t.Error("expected version violation error")
	}
}

func TestRegisterAll(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()

	a := &stubSkill{name: "a", version: "1.0.0"}
	b := &stubSkill{name: "b", version: "1.0.0", deps: []string{"a"}}
	c := &stubSkill{name: "c", version: "1.0.0", deps: []string{"b"}}

	// Register in wrong order — RegisterAll should reorder
	err := mgr.RegisterAll(ctx, []skill.Provider{c, b, a}, skill.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	metas := mgr.List()
	if len(metas) != 3 {
		t.Errorf("expected 3 skills, got %d", len(metas))
	}
}

func TestRegisterAll_CycleError(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()
	a := &stubSkill{name: "a", deps: []string{"b"}}
	b := &stubSkill{name: "b", deps: []string{"a"}}
	err := mgr.RegisterAll(ctx, []skill.Provider{a, b}, skill.Deps{})
	if err == nil {
		t.Error("expected cycle error")
	}
}

// Ensure the errors package is used
var _ = errors.New

// ---- SystemPromptAdditions budget cap tests ---------------------------------

type fixedPromptSkill struct {
	name   string
	prompt string
}

func (s *fixedPromptSkill) Metadata() skill.Metadata {
	return skill.Metadata{Name: s.name, Prompts: []string{s.prompt}}
}
func (s *fixedPromptSkill) Init(_ context.Context, _ skill.Deps) error { return nil }
func (s *fixedPromptSkill) Shutdown(_ context.Context) error           { return nil }

func TestSystemPromptAdditions_TotalCapEnforced(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()

	// Register two skills whose combined body exceeds maxSkillPromptTotalRunes (16000).
	// Each body is 10 000 ASCII runes; together = 20 000 runes > 16 000.
	body := string(make([]rune, 10000))
	for i := range []rune(body) {
		_ = i
	}
	body = ""
	for range 10000 {
		body += "x"
	}

	skillA := &fixedPromptSkill{name: "skill-a", prompt: body}
	skillB := &fixedPromptSkill{name: "skill-b", prompt: body}
	if err := mgr.Register(ctx, skillA, skill.Deps{}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := mgr.Register(ctx, skillB, skill.Deps{}); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	result := mgr.SystemPromptAdditions()
	runes := []rune(result)
	// Total must not exceed cap + truncation marker overhead
	if len(runes) > 16000+len([]rune("\n... [skill prompt truncated]"))+10 {
		t.Errorf("SystemPromptAdditions() rune length %d exceeds cap", len(runes))
	}
}

func TestSystemPromptAdditions_TruncationMarkerPresent(t *testing.T) {
	ctx := context.Background()
	mgr := skill.NewManager()

	// One skill body that is larger than the 16 000-rune total budget.
	body := ""
	for range 20000 {
		body += "y"
	}
	sk := &fixedPromptSkill{name: "big-skill", prompt: body}
	if err := mgr.Register(ctx, sk, skill.Deps{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	result := mgr.SystemPromptAdditions()
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	const marker = "\n... [skill prompt truncated]"
	if len(result) < len(marker) || result[len(result)-len(marker):] != marker {
		t.Errorf("expected result to end with truncation marker, got suffix: %q", result[max(0, len(result)-50):])
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
