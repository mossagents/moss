package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/mossagents/moss/config"
)

// --- fakeSkill for testing ---

type fakeSkill struct {
	name     string
	initErr  error
	shutErr  error
	inited   bool
	shutdown bool
	tools    []string
	prompts  []string
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

func TestMergeConfigs_WithNilInput(t *testing.T) {
	c1 := &Config{Skills: []SkillConfig{{Name: "a", Transport: "stdio", Command: "cmd-a"}}}

	merged := MergeConfigs(nil, c1, nil)
	if merged == nil {
		t.Fatal("expected non-nil merged config")
	}
	if len(merged.Skills) != 1 || merged.Skills[0].Name != "a" {
		t.Fatalf("unexpected merged skills: %+v", merged.Skills)
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

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.yaml")

	cfg := &Config{
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-test123",
		Skills: []SkillConfig{
			{Name: "test", Transport: "stdio", Command: "echo"},
		},
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Reload and verify
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", loaded.Provider)
	}
	if loaded.Model != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %q", loaded.Model)
	}
	if loaded.BaseURL != "https://api.example.com/v1" {
		t.Errorf("expected base_url, got %q", loaded.BaseURL)
	}
	if loaded.APIKey != "sk-test123" {
		t.Errorf("expected api_key, got %q", loaded.APIKey)
	}
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "test" {
		t.Errorf("expected 1 skill, got %+v", loaded.Skills)
	}
}

func TestSaveConfig_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// 写入初始配置
	initial := &Config{
		Provider: "claude",
		APIKey:   "sk-initial",
		Skills: []SkillConfig{
			{Name: "mcp1", Transport: "stdio", Command: "npx server"},
		},
	}
	if err := SaveConfig(path, initial); err != nil {
		t.Fatal(err)
	}

	// 加载、修改、重新保存
	loaded, _ := LoadConfig(path)
	loaded.Provider = "openai"
	loaded.Model = "gpt-4o"
	if err := SaveConfig(path, loaded); err != nil {
		t.Fatal(err)
	}

	// 验证所有字段都被保留
	final, _ := LoadConfig(path)
	if final.Provider != "openai" {
		t.Errorf("provider should be updated to openai, got %q", final.Provider)
	}
	if final.APIKey != "sk-initial" {
		t.Errorf("api_key should be preserved, got %q", final.APIKey)
	}
	if len(final.Skills) != 1 || final.Skills[0].Name != "mcp1" {
		t.Errorf("skills should be preserved, got %+v", final.Skills)
	}
}

func TestLoadConfig_WithProviderFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `provider: openai
model: gpt-4o
base_url: https://api.example.com/v1
api_key: sk-test

skills:
  - name: mcp-server
    transport: stdio
    command: npx -y @test/server
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4o" {
		t.Errorf("unexpected provider/model: %q/%q", cfg.Provider, cfg.Model)
	}
	if cfg.BaseURL != "https://api.example.com/v1" {
		t.Errorf("unexpected base_url: %q", cfg.BaseURL)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("unexpected api_key: %q", cfg.APIKey)
	}
	if len(cfg.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(cfg.Skills))
	}
}

func TestMossDir(t *testing.T) {
	dir := AppDir()
	if dir == "" {
		t.Skip("cannot determine home directory")
	}
	if !strings.HasSuffix(dir, ".moss") {
		t.Errorf("expected path ending with .moss, got %q", dir)
	}
}

func TestEnsureMossDir_CreatesTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("moss")
	t.Cleanup(func() { SetAppName("moss") })

	if err := EnsureAppDir(); err != nil {
		t.Fatalf("EnsureAppDir failed: %v", err)
	}

	path := DefaultGlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	if !strings.Contains(string(data), "skills:") {
		t.Fatalf("template should contain skills section, got: %q", string(data))
	}

	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("generated template should be parseable, got error: %v", err)
	}
}

func TestEnsureMossDir_DoesNotOverwriteExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("moss")
	t.Cleanup(func() { SetAppName("moss") })

	if err := os.MkdirAll(AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	existing := "provider: openai\nmodel: qwen\n"
	if err := os.WriteFile(DefaultGlobalConfigPath(), []byte(existing), 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if err := EnsureAppDir(); err != nil {
		t.Fatalf("EnsureAppDir failed: %v", err)
	}

	data, err := os.ReadFile(DefaultGlobalConfigPath())
	if err != nil {
		t.Fatalf("read config after ensure: %v", err)
	}
	if string(data) != existing {
		t.Fatalf("existing config should be preserved, got: %q", string(data))
	}
}

func TestLoadGlobalConfig_YAMLOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosscode")
	t.Cleanup(func() { SetAppName("moss") })

	if err := os.MkdirAll(AppDir(), 0700); err != nil {
		t.Fatalf("prepare dir: %v", err)
	}
	content := "provider: openai\nmodel: gpt-4o\n"
	if err := os.WriteFile(DefaultGlobalConfigPath(), []byte(content), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig failed: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4o" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadGlobalConfig_MissingReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosscode")
	t.Cleanup(func() { SetAppName("moss") })

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Provider != "" || cfg.Model != "" || len(cfg.Skills) != 0 {
		t.Fatalf("expected empty config for missing config.yaml, got: %+v", cfg)
	}
}

func TestEnsureMossDir_CreatesYAMLByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosscode")
	t.Cleanup(func() { SetAppName("moss") })

	if err := EnsureAppDir(); err != nil {
		t.Fatalf("EnsureAppDir failed: %v", err)
	}

	if _, err := os.Stat(DefaultGlobalConfigPath()); err != nil {
		t.Fatalf("config.yaml should be created by default, got err: %v", err)
	}
}

func TestRenderSystemPrompt_UsesGlobalTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosscode")
	t.Cleanup(func() { SetAppName("moss") })

	if err := os.MkdirAll(AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	globalTpl := "You are {{.App}} on {{.OS}} in {{.Workspace}}"
	if err := os.WriteFile(DefaultGlobalSystemPromptTemplatePath(), []byte(globalTpl), 0600); err != nil {
		t.Fatalf("write global template: %v", err)
	}

	rendered := RenderSystemPrompt(".", "default {{.App}}", map[string]any{
		"App":       "mosscode",
		"OS":        "windows",
		"Workspace": ".",
	})
	if rendered != "You are mosscode on windows in ." {
		t.Fatalf("unexpected rendered prompt: %q", rendered)
	}
}

func TestRenderSystemPrompt_ProjectTemplateOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosswork")
	t.Cleanup(func() { SetAppName("moss") })

	workspace := t.TempDir()

	if err := os.MkdirAll(AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	if err := os.WriteFile(DefaultGlobalSystemPromptTemplatePath(), []byte("global {{.Scope}}"), 0600); err != nil {
		t.Fatalf("write global template: %v", err)
	}

	projectPath := DefaultProjectSystemPromptTemplatePath(workspace)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0700); err != nil {
		t.Fatalf("prepare project template dir: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("project {{.Scope}}"), 0600); err != nil {
		t.Fatalf("write project template: %v", err)
	}

	rendered := RenderSystemPrompt(workspace, "default {{.Scope}}", map[string]any{"Scope": "prompt"})
	if rendered != "project prompt" {
		t.Fatalf("project template should override global template, got: %q", rendered)
	}
}

func TestRenderSystemPromptForTrust_RestrictedIgnoresProjectTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetAppName("mosswork")
	t.Cleanup(func() { SetAppName("moss") })

	workspace := t.TempDir()

	if err := os.MkdirAll(AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	if err := os.WriteFile(DefaultGlobalSystemPromptTemplatePath(), []byte("global {{.Scope}}"), 0600); err != nil {
		t.Fatalf("write global template: %v", err)
	}

	projectPath := DefaultProjectSystemPromptTemplatePath(workspace)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0700); err != nil {
		t.Fatalf("prepare project template dir: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("project {{.Scope}}"), 0600); err != nil {
		t.Fatalf("write project template: %v", err)
	}

	rendered := RenderSystemPromptForTrust(workspace, TrustRestricted, "default {{.Scope}}", map[string]any{"Scope": "prompt"})
	if rendered != "global prompt" {
		t.Fatalf("restricted mode should ignore project template, got: %q", rendered)
	}
}

func TestNormalizeTrustLevel(t *testing.T) {
	tests := map[string]string{
		"":           TrustTrusted,
		"trusted":    TrustTrusted,
		"restricted": TrustRestricted,
		"unknown":    TrustRestricted,
	}
	for input, want := range tests {
		if got := NormalizeTrustLevel(input); got != want {
			t.Fatalf("NormalizeTrustLevel(%q)=%q, want %q", input, got, want)
		}
	}
}
