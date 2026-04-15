package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProviderIdentity(t *testing.T) {
	identity := NormalizeProviderIdentity("openai", "deepseek")
	if identity.APIType != APITypeOpenAICompletions {
		t.Fatalf("APIType = %q, want %s", identity.APIType, APITypeOpenAICompletions)
	}
	if identity.Provider != APITypeOpenAICompletions {
		t.Fatalf("Provider = %q, want %s", identity.Provider, APITypeOpenAICompletions)
	}
	if identity.Name != "deepseek" {
		t.Fatalf("Name = %q, want deepseek", identity.Name)
	}
	if identity.EffectiveAPIType() != APITypeOpenAICompletions {
		t.Fatalf("EffectiveAPIType = %q, want %s", identity.EffectiveAPIType(), APITypeOpenAICompletions)
	}
	if identity.DisplayName() != "deepseek" {
		t.Fatalf("DisplayName = %q, want deepseek", identity.DisplayName())
	}
	if identity.Label() != "deepseek (openai-completions)" {
		t.Fatalf("Label = %q, want deepseek (openai-completions)", identity.Label())
	}
}

func TestNormalizeProviderIdentity_OpenAIResponses(t *testing.T) {
	identity := NormalizeProviderIdentity("openai-responses", "")
	if identity.APIType != APITypeOpenAIResponses {
		t.Fatalf("APIType = %q, want %s", identity.APIType, APITypeOpenAIResponses)
	}
	if identity.Provider != APITypeOpenAIResponses {
		t.Fatalf("Provider = %q, want %s", identity.Provider, APITypeOpenAIResponses)
	}
	if identity.Name != APITypeOpenAIResponses {
		t.Fatalf("Name = %q, want %s", identity.Name, APITypeOpenAIResponses)
	}
}

func TestConfigProviderIdentitySynthesizesDefaultModelFromTopLevelFields(t *testing.T) {
	cfg := &Config{Provider: "claude"}
	cfg.normalizeProviderFields()
	if cfg.Provider != "claude" {
		t.Fatalf("Provider = %q, want claude", cfg.Provider)
	}
	if cfg.Name != "claude" {
		t.Fatalf("Name = %q, want claude", cfg.Name)
	}
	if len(cfg.Models) != 1 || !cfg.Models[0].Default {
		t.Fatalf("expected top-level provider fields to synthesize one default model, got %+v", cfg.Models)
	}
}

func TestSkillConfigIsRequiredDefaultFalse(t *testing.T) {
	var sc SkillConfig
	if sc.IsRequired() {
		t.Fatal("IsRequired should be false by default")
	}
}

func TestSkillConfigIsRequiredHonorsValue(t *testing.T) {
	vTrue := true
	vFalse := false
	if !(SkillConfig{Required: &vTrue}).IsRequired() {
		t.Fatal("IsRequired should be true when required=true")
	}
	if (SkillConfig{Required: &vFalse}).IsRequired() {
		t.Fatal("IsRequired should be false when required=false")
	}
}

func TestDefaultProjectConfigPathUsesDotMossDir(t *testing.T) {
	workspace := filepath.Join("C:\\", "workspace")
	got := DefaultProjectConfigPath(workspace)
	want := filepath.Join(workspace, ".moss", "config.yaml")
	if got != want {
		t.Fatalf("DefaultProjectConfigPath() = %q, want %q", got, want)
	}
}

func TestSaveAndLoadProjectConfigUsesDotMossDir(t *testing.T) {
	workspace := t.TempDir()
	cfg := &Config{DefaultProfile: "guarded"}
	path := DefaultProjectConfigPath(workspace)

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".moss")); err != nil {
		t.Fatalf("Stat(.moss) = %v", err)
	}

	loaded, err := LoadProjectConfig(workspace)
	if err != nil {
		t.Fatalf("LoadProjectConfig() = %v", err)
	}
	if loaded.DefaultProfile != "guarded" {
		t.Fatalf("DefaultProfile = %q, want guarded", loaded.DefaultProfile)
	}
}

func TestLoadProjectConfigIgnoresLegacyRootMossYAML(t *testing.T) {
	workspace := t.TempDir()
	legacyPath := filepath.Join(workspace, "moss.yaml")
	if err := os.WriteFile(legacyPath, []byte("default_profile: legacy\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) = %v", legacyPath, err)
	}

	loaded, err := LoadProjectConfig(workspace)
	if err != nil {
		t.Fatalf("LoadProjectConfig() = %v", err)
	}
	if loaded.DefaultProfile != "" {
		t.Fatalf("DefaultProfile = %q, want empty", loaded.DefaultProfile)
	}
}

// ─── NormalizeTrustLevel / ProjectAssetsAllowed ───────────────────────────

func TestNormalizeTrustLevel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", TrustTrusted},
		{"trusted", TrustTrusted},
		{"TRUSTED", TrustTrusted},
		{"restricted", TrustRestricted},
		{"RESTRICTED", TrustRestricted},
		{"unknown", TrustRestricted},
	}
	for _, c := range cases {
		got := NormalizeTrustLevel(c.in)
		if got != c.want {
			t.Errorf("NormalizeTrustLevel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProjectAssetsAllowed(t *testing.T) {
	if !ProjectAssetsAllowed(TrustTrusted) {
		t.Fatal("trusted should allow project assets")
	}
	if ProjectAssetsAllowed(TrustRestricted) {
		t.Fatal("restricted should not allow project assets")
	}
}

// ─── SetAppName / AppName ─────────────────────────────────────────────────

func TestSetAndGetAppName(t *testing.T) {
	orig := AppName()
	t.Cleanup(func() { SetAppName(orig) })

	SetAppName("myapp")
	if got := AppName(); got != "myapp" {
		t.Fatalf("AppName() = %q, want myapp", got)
	}
}

// ─── AppDir / DefaultGlobalConfigPath ─────────────────────────────────────

func TestAppDir_NotEmpty(t *testing.T) {
	orig := AppName()
	t.Cleanup(func() { SetAppName(orig) })
	SetAppName("moss")
	if d := AppDir(); d == "" {
		t.Fatal("AppDir() should be non-empty on a system with a home dir")
	}
}

func TestDefaultGlobalConfigPath_NonEmpty(t *testing.T) {
	if p := DefaultGlobalConfigPath(); p == "" {
		t.Fatal("DefaultGlobalConfigPath() should be non-empty")
	}
}

// ─── SkillConfig.IsEnabled / IsMCP ────────────────────────────────────────

func TestSkillConfigIsEnabledDefaultTrue(t *testing.T) {
	var sc SkillConfig
	if !sc.IsEnabled() {
		t.Fatal("IsEnabled should default to true")
	}
}

func TestSkillConfigIsEnabledHonorsPointer(t *testing.T) {
	f := false
	sc := SkillConfig{Enabled: &f}
	if sc.IsEnabled() {
		t.Fatal("IsEnabled should be false when pointer is false")
	}
}

func TestSkillConfigIsMCP(t *testing.T) {
	if !(SkillConfig{Transport: "stdio"}).IsMCP() {
		t.Fatal("stdio transport should be MCP")
	}
	if !(SkillConfig{Transport: "sse"}).IsMCP() {
		t.Fatal("sse transport should be MCP")
	}
	if (SkillConfig{Transport: "http"}).IsMCP() {
		t.Fatal("http transport should not be MCP")
	}
}

// ─── MergeConfigs ─────────────────────────────────────────────────────────

func TestMergeConfigs_DeduplicatesSkills(t *testing.T) {
	a := &Config{Skills: []SkillConfig{{Name: "tool-a"}, {Name: "tool-b"}}}
	b := &Config{Skills: []SkillConfig{{Name: "tool-b", Command: "new-cmd"}, {Name: "tool-c"}}}
	merged := MergeConfigs(a, b)
	if len(merged.Skills) != 3 {
		t.Fatalf("expected 3 unique skills, got %d", len(merged.Skills))
	}
	var toolB SkillConfig
	for _, s := range merged.Skills {
		if s.Name == "tool-b" {
			toolB = s
		}
	}
	if toolB.Command != "new-cmd" {
		t.Fatalf("later config should overwrite earlier for same skill name, got %q", toolB.Command)
	}
}

func TestMergeConfigs_NilInputIgnored(t *testing.T) {
	a := &Config{Skills: []SkillConfig{{Name: "x"}}}
	merged := MergeConfigs(nil, a, nil)
	if len(merged.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(merged.Skills))
	}
}

// ─── Config.ProviderIdentity / EffectiveAPIType / DisplayProviderName ──────

func TestConfig_ProviderIdentityNilSafe(t *testing.T) {
	var c *Config
	id := c.ProviderIdentity()
	if id.APIType != "" {
		t.Fatalf("nil config should return empty ProviderIdentity")
	}
}

func TestConfig_EffectiveAPIType(t *testing.T) {
	c := &Config{Provider: "anthropic"}
	c.normalizeProviderFields()
	if got := c.EffectiveAPIType(); got != APITypeClaude {
		t.Fatalf("EffectiveAPIType = %q, want %s", got, APITypeClaude)
	}
}

func TestConfig_DisplayProviderName(t *testing.T) {
	c := &Config{Provider: "openai", Name: "my-openai"}
	c.normalizeProviderFields()
	name := c.DisplayProviderName()
	if name == "" {
		t.Fatal("DisplayProviderName should not be empty")
	}
}

// ─── ProviderIdentity.Label edge cases ────────────────────────────────────

func TestProviderIdentityLabel_SameName(t *testing.T) {
	id := ProviderIdentity{APIType: "claude", Provider: "claude", Name: "claude"}
	if id.Label() != "claude" {
		t.Fatalf("Label() = %q, want claude", id.Label())
	}
}

func TestProviderIdentityLabel_EmptyName(t *testing.T) {
	id := ProviderIdentity{APIType: "claude", Provider: "claude", Name: ""}
	if id.Label() != "claude" {
		t.Fatalf("Label() = %q, want claude", id.Label())
	}
}

func TestProviderIdentityLabel_EmptyAPIType(t *testing.T) {
	id := ProviderIdentity{APIType: "", Provider: "", Name: "mybot"}
	if id.Label() != "mybot" {
		t.Fatalf("Label() = %q, want mybot", id.Label())
	}
}

// ─── DefaultProjectSystemPromptTemplatePath ───────────────────────────────

func TestDefaultProjectSystemPromptTemplatePath(t *testing.T) {
	orig := AppName()
	t.Cleanup(func() { SetAppName(orig) })
	SetAppName("moss")
	p := DefaultProjectSystemPromptTemplatePath("/workspace")
	if p == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.Contains(p, "system_prompt.tmpl") {
		t.Fatalf("expected system_prompt.tmpl in path, got %q", p)
	}
}

// ─── DefaultTemplateContext ────────────────────────────────────────────────

func TestDefaultTemplateContext(t *testing.T) {
	ctx := DefaultTemplateContext("/my/workspace")
	if ctx["Workspace"] != "/my/workspace" {
		t.Fatalf("Workspace = %v", ctx["Workspace"])
	}
	if ctx["OS"] == "" {
		t.Fatal("OS should be non-empty")
	}
	if ctx["Shell"] == "" {
		t.Fatal("Shell should be non-empty")
	}
}

// ─── RenderSystemPrompt / renderPromptTemplate ────────────────────────────

func TestRenderSystemPromptForTrust_UsesDefaultTemplate(t *testing.T) {
	result := RenderSystemPromptForTrust(t.TempDir(), TrustTrusted, "Hello {{.Name}}", map[string]any{"Name": "World"})
	if result != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", result)
	}
}

func TestRenderSystemPromptForTrust_BrokenTemplate_FallsBackToDefault(t *testing.T) {
	result := RenderSystemPromptForTrust(t.TempDir(), TrustTrusted, "plain default", map[string]any{})
	if result != "plain default" {
		t.Fatalf("expected 'plain default', got %q", result)
	}
}

func TestRenderSystemPrompt_UsesProjectTemplate(t *testing.T) {
	workspace := t.TempDir()
	dir := DefaultProjectConfigDir(workspace)
	_ = os.MkdirAll(dir, 0700)
	tplPath := filepath.Join(workspace, ".moss", "system_prompt.tmpl")
	_ = os.WriteFile(tplPath, []byte("Project: {{.Name}}"), 0600)

	result := RenderSystemPrompt(workspace, "Default: {{.Name}}", map[string]any{"Name": "Moss"})
	if result != "Project: Moss" {
		t.Fatalf("expected project template to win, got %q", result)
	}
}

// ─── LoadProjectConfigForTrust ────────────────────────────────────────────

func TestLoadProjectConfigForTrust_Restricted_ReturnsEmpty(t *testing.T) {
	workspace := t.TempDir()
	path := DefaultProjectConfigPath(workspace)
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte("default_profile: guarded\n"), 0600)

	cfg, err := LoadProjectConfigForTrust(workspace, TrustRestricted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultProfile != "" {
		t.Fatalf("restricted trust should skip project config, got %q", cfg.DefaultProfile)
	}
}

func TestLoadProjectConfigForTrust_Trusted_ReadsConfig(t *testing.T) {
	workspace := t.TempDir()
	path := DefaultProjectConfigPath(workspace)
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte("default_profile: guarded\n"), 0600)

	cfg, err := LoadProjectConfigForTrust(workspace, TrustTrusted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultProfile != "guarded" {
		t.Fatalf("trusted should read project config, got %q", cfg.DefaultProfile)
	}
}
