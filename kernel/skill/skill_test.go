package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// --- fakeSkill for testing ---

type fakeSkill struct {
	name      string
	initErr   error
	shutErr   error
	inited    bool
	shutdown  bool
	tools     []string
	prompts   []string
}

func (f *fakeSkill) Metadata() Metadata {
	return Metadata{
		Name:        f.name,
		Version:     "1.0.0",
		Description: "fake skill for testing",
		Tools:       f.tools,
		Prompts:     f.prompts,
	}
}

func (f *fakeSkill) Init(_ context.Context, _ Deps) error {
	if f.initErr != nil {
		return f.initErr
	}
	f.inited = true
	return nil
}

func (f *fakeSkill) Shutdown(_ context.Context) error {
	f.shutdown = true
	return f.shutErr
}

// --- Manager tests ---

func TestManager_RegisterAndList(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	s1 := &fakeSkill{name: "alpha", tools: []string{"tool_a"}, prompts: []string{"prompt_a"}}
	s2 := &fakeSkill{name: "beta", tools: []string{"tool_b"}}

	if err := m.Register(ctx, s1, Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(ctx, s2, Deps{}); err != nil {
		t.Fatal(err)
	}

	if !s1.inited || !s2.inited {
		t.Error("skills should be initialized after Register")
	}

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "beta" {
		t.Errorf("unexpected order: %v, %v", list[0].Name, list[1].Name)
	}
}

func TestManager_RegisterDuplicate(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	s := &fakeSkill{name: "dup"}
	if err := m.Register(ctx, s, Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(ctx, &fakeSkill{name: "dup"}, Deps{}); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestManager_RegisterInitFail(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	s := &fakeSkill{name: "fail", initErr: context.Canceled}
	if err := m.Register(ctx, s, Deps{}); err == nil {
		t.Error("expected error when Init fails")
	}
	// should be cleaned up
	if _, ok := m.Get("fail"); ok {
		t.Error("skill should be removed on Init failure")
	}
}

func TestManager_Unregister(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	s := &fakeSkill{name: "rem"}
	_ = m.Register(ctx, s, Deps{})

	if err := m.Unregister(ctx, "rem"); err != nil {
		t.Fatal(err)
	}
	if !s.shutdown {
		t.Error("Shutdown should be called on Unregister")
	}
	if _, ok := m.Get("rem"); ok {
		t.Error("skill should not exist after Unregister")
	}
}

func TestManager_ShutdownAll(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	s1 := &fakeSkill{name: "a"}
	s2 := &fakeSkill{name: "b"}
	_ = m.Register(ctx, s1, Deps{})
	_ = m.Register(ctx, s2, Deps{})

	if err := m.ShutdownAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !s1.shutdown || !s2.shutdown {
		t.Error("all skills should be shut down")
	}
	if len(m.List()) != 0 {
		t.Error("all skills should be removed after ShutdownAll")
	}
}

func TestManager_SystemPromptAdditions(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	_ = m.Register(ctx, &fakeSkill{name: "a", prompts: []string{"hello"}}, Deps{})
	_ = m.Register(ctx, &fakeSkill{name: "b", prompts: []string{"world"}}, Deps{})

	additions := m.SystemPromptAdditions()
	if additions != "hello\n\nworld" {
		t.Errorf("unexpected additions: %q", additions)
	}
}

// --- Config tests ---

func TestLoadConfig_NotExist(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills) != 0 {
		t.Error("expected empty config for nonexistent file")
	}
}

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `skills:
  - name: test-server
    transport: stdio
    command: npx -y @test/server
    env:
      KEY: value
  - name: disabled
    transport: sse
    url: http://localhost:3000
    enabled: false
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(cfg.Skills))
	}

	s := cfg.Skills[0]
	if s.Name != "test-server" || s.Transport != "stdio" || s.Command != "npx -y @test/server" {
		t.Errorf("unexpected skill 0: %+v", s)
	}
	if s.Env["KEY"] != "value" {
		t.Errorf("expected env KEY=value, got %v", s.Env)
	}
	if !s.IsEnabled() || !s.IsMCP() {
		t.Error("test-server should be enabled MCP")
	}

	d := cfg.Skills[1]
	if d.IsEnabled() {
		t.Error("disabled skill should not be enabled")
	}
}

func TestMergeConfigs(t *testing.T) {
	c1 := &Config{Skills: []SkillConfig{
		{Name: "a", Transport: "stdio", Command: "cmd-a"},
		{Name: "b", Transport: "sse", URL: "http://b"},
	}}
	c2 := &Config{Skills: []SkillConfig{
		{Name: "b", Transport: "stdio", Command: "cmd-b-override"},
		{Name: "c", Transport: "stdio", Command: "cmd-c"},
	}}

	merged := MergeConfigs(c1, c2)
	if len(merged.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(merged.Skills))
	}

	// b should be overridden by c2
	for _, s := range merged.Skills {
		if s.Name == "b" {
			if s.Command != "cmd-b-override" {
				t.Errorf("expected b to be overridden, got command %q", s.Command)
			}
		}
	}
}

func TestSkillConfig_IsEnabled_Default(t *testing.T) {
	sc := SkillConfig{Name: "test"}
	if !sc.IsEnabled() {
		t.Error("should be enabled by default when Enabled is nil")
	}
}

func TestSkillConfig_IsMCP(t *testing.T) {
	if sc := (SkillConfig{Transport: "stdio"}); !sc.IsMCP() {
		t.Error("stdio should be MCP")
	}
	if sc := (SkillConfig{Transport: "sse"}); !sc.IsMCP() {
		t.Error("sse should be MCP")
	}
	if sc := (SkillConfig{Transport: ""}); sc.IsMCP() {
		t.Error("empty transport should not be MCP")
	}
}
