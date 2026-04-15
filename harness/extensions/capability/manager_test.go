package capability_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mossagents/moss/harness/extensions/capability"
)

type stubProvider struct {
	name     string
	version  string
	requires []capability.Dependency
	deps     []string
	initErr  error
	shutErr  error
	prompts  []string
	tools    []string
	inited   bool
	shutdown bool
}

func (s *stubProvider) Metadata() capability.Metadata {
	version := s.version
	if version == "" {
		version = "1.0.0"
	}
	return capability.Metadata{
		Name:        s.name,
		Version:     version,
		Description: "stub capability for testing",
		Tools:       append([]string(nil), s.tools...),
		Prompts:     append([]string(nil), s.prompts...),
		DependsOn:   append([]string(nil), s.deps...),
		Requires:    append([]capability.Dependency(nil), s.requires...),
	}
}

func (s *stubProvider) Init(_ context.Context, _ capability.Deps) error {
	if s.initErr != nil {
		return s.initErr
	}
	s.inited = true
	return nil
}

func (s *stubProvider) Shutdown(_ context.Context) error {
	s.shutdown = true
	return s.shutErr
}

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
		got, err := capability.ParseVersion(c.input)
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
	if capability.CompareVersion("1.0.0", "2.0.0") >= 0 {
		t.Error("expected 1.0.0 < 2.0.0")
	}
	if capability.CompareVersion("2.0.0", "1.9.9") <= 0 {
		t.Error("expected 2.0.0 > 1.9.9")
	}
	if capability.CompareVersion("1.2.3", "1.2.3") != 0 {
		t.Error("expected 1.2.3 == 1.2.3")
	}
}

func TestIsVersionInRange(t *testing.T) {
	if !capability.IsVersionInRange("1.5.0", "1.0.0", "2.0.0") {
		t.Error("1.5.0 should be in [1.0.0, 2.0.0]")
	}
	if capability.IsVersionInRange("0.9.0", "1.0.0", "2.0.0") {
		t.Error("0.9.0 should not be in [1.0.0, 2.0.0]")
	}
	if capability.IsVersionInRange("2.1.0", "1.0.0", "2.0.0") {
		t.Error("2.1.0 should not be in [1.0.0, 2.0.0]")
	}
	if !capability.IsVersionInRange("1.0.0", "", "2.0.0") {
		t.Error("1.0.0 should be in [*, 2.0.0]")
	}
}

func TestManagerRegisterAndList(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()

	a := &stubProvider{name: "alpha", tools: []string{"tool_a"}, prompts: []string{"prompt_a"}}
	b := &stubProvider{name: "beta", tools: []string{"tool_b"}}

	if err := m.Register(ctx, a, capability.Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(ctx, b, capability.Deps{}); err != nil {
		t.Fatal(err)
	}

	if !a.inited || !b.inited {
		t.Fatal("providers should be initialized after Register")
	}

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "beta" {
		t.Fatalf("unexpected order: %v", list)
	}
}

func TestManagerRegisterDuplicate(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()
	if err := m.Register(ctx, &stubProvider{name: "dup"}, capability.Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(ctx, &stubProvider{name: "dup"}, capability.Deps{}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestManagerRegisterInitFail(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()
	if err := m.Register(ctx, &stubProvider{name: "fail", initErr: context.Canceled}, capability.Deps{}); err == nil {
		t.Fatal("expected init failure")
	}
	if _, ok := m.Get("fail"); ok {
		t.Fatal("provider should be removed on Init failure")
	}
}

func TestManagerUnregister(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()
	p := &stubProvider{name: "rem"}
	_ = m.Register(ctx, p, capability.Deps{})

	if err := m.Unregister(ctx, "rem"); err != nil {
		t.Fatal(err)
	}
	if !p.shutdown {
		t.Fatal("Shutdown should be called on Unregister")
	}
	if _, ok := m.Get("rem"); ok {
		t.Fatal("provider should not exist after Unregister")
	}
}

func TestManagerShutdownAll(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()
	a := &stubProvider{name: "a"}
	b := &stubProvider{name: "b"}
	_ = m.Register(ctx, a, capability.Deps{})
	_ = m.Register(ctx, b, capability.Deps{})

	if err := m.ShutdownAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !a.shutdown || !b.shutdown {
		t.Fatal("all providers should be shut down")
	}
	if len(m.List()) != 0 {
		t.Fatal("all providers should be removed after ShutdownAll")
	}
}

func TestManagerSystemPromptAdditions(t *testing.T) {
	m := capability.NewManager()
	ctx := context.Background()
	_ = m.Register(ctx, &stubProvider{name: "a", prompts: []string{"hello"}}, capability.Deps{})
	_ = m.Register(ctx, &stubProvider{name: "b", prompts: []string{"world"}}, capability.Deps{})

	if got := m.SystemPromptAdditions(); got != "hello\n\nworld" {
		t.Fatalf("unexpected additions: %q", got)
	}
}

func TestTopologicalSortNoDeps(t *testing.T) {
	a := &stubProvider{name: "a"}
	b := &stubProvider{name: "b"}
	sorted, err := capability.TopologicalSort([]capability.Provider{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(sorted))
	}
}

func TestTopologicalSortWithDeps(t *testing.T) {
	a := &stubProvider{name: "a"}
	b := &stubProvider{name: "b", deps: []string{"a"}}
	sorted, err := capability.TopologicalSort([]capability.Provider{b, a})
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int{}
	for i, p := range sorted {
		order[p.Metadata().Name] = i
	}
	if order["a"] >= order["b"] {
		t.Fatalf("expected a before b, got %v", order)
	}
}

func TestTopologicalSortCycleDetection(t *testing.T) {
	a := &stubProvider{name: "a", deps: []string{"b"}}
	b := &stubProvider{name: "b", deps: []string{"a"}}
	if _, err := capability.TopologicalSort([]capability.Provider{a, b}); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestValidateDepsVersionConstraint(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()

	base := &stubProvider{name: "base", version: "1.5.0"}
	if err := mgr.Register(ctx, base, capability.Deps{}); err != nil {
		t.Fatal(err)
	}

	dep := &stubProvider{
		name:     "dep",
		version:  "1.0.0",
		requires: []capability.Dependency{{Name: "base", MinVersion: "1.0.0", MaxVersion: "2.0.0"}},
	}
	if err := mgr.Register(ctx, dep, capability.Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ValidateDeps(dep); err != nil {
		t.Fatalf("expected valid deps, got %v", err)
	}
}

func TestValidateDepsVersionViolation(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()

	base := &stubProvider{name: "base", version: "0.5.0"}
	_ = mgr.Register(ctx, base, capability.Deps{})

	dep := &stubProvider{
		name:     "dep",
		requires: []capability.Dependency{{Name: "base", MinVersion: "1.0.0"}},
	}
	_ = mgr.Register(ctx, dep, capability.Deps{})
	if err := mgr.ValidateDeps(dep); err == nil {
		t.Fatal("expected version violation error")
	}
}

func TestRegisterAll(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()

	a := &stubProvider{name: "a", version: "1.0.0"}
	b := &stubProvider{name: "b", version: "1.0.0", deps: []string{"a"}}
	c := &stubProvider{name: "c", version: "1.0.0", deps: []string{"b"}}

	if err := mgr.RegisterAll(ctx, []capability.Provider{c, b, a}, capability.Deps{}); err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.List()); got != 3 {
		t.Fatalf("expected 3 providers, got %d", got)
	}
}

func TestRegisterAllCycleError(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()
	a := &stubProvider{name: "a", deps: []string{"b"}}
	b := &stubProvider{name: "b", deps: []string{"a"}}
	if err := mgr.RegisterAll(ctx, []capability.Provider{a, b}, capability.Deps{}); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestSystemPromptAdditionsTotalCapEnforced(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()
	body := strings.Repeat("x", 10000)

	a := &stubProvider{name: "a", prompts: []string{body}}
	b := &stubProvider{name: "b", prompts: []string{body}}
	if err := mgr.Register(ctx, a, capability.Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Register(ctx, b, capability.Deps{}); err != nil {
		t.Fatal(err)
	}

	result := mgr.SystemPromptAdditions()
	limit := 16000 + len([]rune("\n... [capability prompt truncated]")) + 10
	if got := len([]rune(result)); got > limit {
		t.Fatalf("SystemPromptAdditions() rune length %d exceeds cap", got)
	}
}

func TestSystemPromptAdditionsTruncationMarkerPresent(t *testing.T) {
	ctx := context.Background()
	mgr := capability.NewManager()
	body := strings.Repeat("y", 20000)

	if err := mgr.Register(ctx, &stubProvider{name: "big", prompts: []string{body}}, capability.Deps{}); err != nil {
		t.Fatal(err)
	}

	result := mgr.SystemPromptAdditions()
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	const marker = "\n... [capability prompt truncated]"
	if !strings.HasSuffix(result, marker) {
		t.Fatalf("expected result to end with truncation marker, got suffix %q", result[max(0, len(result)-50):])
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = errors.New
