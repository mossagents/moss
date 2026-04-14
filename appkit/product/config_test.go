package product

import (
	appconfig "github.com/mossagents/moss/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyConfigSetAndUnset(t *testing.T) {
	cfg := &appconfig.Config{}
	display, err := applyConfigSet(cfg, "provider", "openai", false)
	if err != nil {
		t.Fatalf("set provider: %v", err)
	}
	if display != appconfig.APITypeOpenAICompletions {
		t.Fatalf("expected provider display %s, got %q", appconfig.APITypeOpenAICompletions, display)
	}
	if cfg.EffectiveAPIType() != appconfig.APITypeOpenAICompletions {
		t.Fatalf("expected provider=%s, got %q", appconfig.APITypeOpenAICompletions, cfg.EffectiveAPIType())
	}
	if _, err := applyConfigSet(cfg, "model", "gpt-5", false); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if cfg.Model != "gpt-5" {
		t.Fatalf("expected model gpt-5, got %q", cfg.Model)
	}
	if err := applyConfigUnset(cfg, "model", false); err != nil {
		t.Fatalf("unset model: %v", err)
	}
	if cfg.Model != "" {
		t.Fatalf("expected empty model, got %q", cfg.Model)
	}
}

func TestListMCPServersIncludesSourceAndTrustStatus(t *testing.T) {
	tempHome := t.TempDir()
	workspace := filepath.Join(tempHome, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	appconfig.SetAppName("moss-product-test")
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)

	globalCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{
			{Name: "global-mcp", Transport: "stdio", Command: "node global.js"},
			{Name: "shared-mcp", Transport: "stdio", Command: "node global-shared.js"},
		},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultGlobalConfigPath(), globalCfg); err != nil {
		t.Fatalf("SaveConfig global: %v", err)
	}
	disabled := false
	projectCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{
			{Name: "shared-mcp", Transport: "stdio", Command: "node project-shared.js", Enabled: &disabled},
			{Name: "project-mcp", Transport: "sse", URL: "https://example.test/mcp"},
		},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultProjectConfigPath(workspace), projectCfg); err != nil {
		t.Fatalf("SaveConfig project: %v", err)
	}

	restricted, err := ListMCPServers(workspace, appconfig.TrustRestricted)
	if err != nil {
		t.Fatalf("ListMCPServers restricted: %v", err)
	}
	if len(restricted) != 2 {
		t.Fatalf("restricted mcp count = %d, want 2", len(restricted))
	}
	assertMCPStatus(t, restricted, "global-mcp", MCPConfigSourceGlobal, "enabled", true)
	assertMCPNotPresent(t, restricted, "project-mcp")

	trusted, err := ListMCPServers(workspace, appconfig.TrustTrusted)
	if err != nil {
		t.Fatalf("ListMCPServers trusted: %v", err)
	}
	assertMCPStatus(t, trusted, "shared-mcp", MCPConfigSourceGlobal, "shadowed", false)
	assertMCPStatus(t, trusted, "shared-mcp", MCPConfigSourceProject, "disabled", false)
}

func TestSetMCPEnabledUpdatesProjectScope(t *testing.T) {
	tempHome := t.TempDir()
	workspace := filepath.Join(tempHome, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	appconfig.SetAppName("moss-product-test")
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)

	projectCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{
			{Name: "project-mcp", Transport: "stdio", Command: "node server.js"},
		},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultProjectConfigPath(workspace), projectCfg); err != nil {
		t.Fatalf("SaveConfig project: %v", err)
	}

	server, err := SetMCPEnabled(workspace, "project-mcp", "project", false)
	if err != nil {
		t.Fatalf("SetMCPEnabled: %v", err)
	}
	if server.Enabled {
		t.Fatal("expected server to be disabled")
	}
	updated, err := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspace))
	if err != nil {
		t.Fatalf("LoadConfig project: %v", err)
	}
	if len(updated.Skills) != 1 || updated.Skills[0].Enabled == nil || *updated.Skills[0].Enabled {
		t.Fatalf("updated project config = %+v, want disabled MCP", updated.Skills)
	}
}

func assertMCPStatus(t *testing.T, servers []MCPServerConfigView, name string, source MCPConfigSource, status string, effective bool) {
	t.Helper()
	for _, server := range servers {
		if server.Name == name && server.Source == source {
			if server.Status != status {
				t.Fatalf("%s [%s] status = %q, want %q", name, source, server.Status, status)
			}
			if server.Effective != effective {
				t.Fatalf("%s [%s] effective = %t, want %t", name, source, server.Effective, effective)
			}
			return
		}
	}
	t.Fatalf("mcp server %s [%s] not found in %+v", name, source, servers)
}

func assertMCPNotPresent(t *testing.T, servers []MCPServerConfigView, name string) {
	t.Helper()
	for _, server := range servers {
		if server.Name == name {
			t.Fatalf("unexpected mcp server %s in %+v", name, servers)
		}
	}
}

// ── pure rendering / helper functions ────────────────────────────────────────

func TestRenderMCPServerList_Empty(t *testing.T) {
	got := RenderMCPServerList(nil)
	if got != "No MCP servers configured.\n" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderMCPServerList_Items(t *testing.T) {
	servers := []MCPServerConfigView{
		{Name: "mcp1", Source: MCPConfigSourceGlobal, Transport: "stdio", Enabled: true, Effective: true, Status: "ok"},
		{Name: "mcp2", Source: MCPConfigSourceProject, Transport: "sse", Enabled: false, Effective: false, Status: "disabled", Target: "http://localhost:8080", HasEnv: true, EnvKeys: []string{"KEY1", "KEY2"}},
	}
	got := RenderMCPServerList(servers)
	if !strings.Contains(got, "mcp1") || !strings.Contains(got, "mcp2") {
		t.Error("missing server names")
	}
	if !strings.Contains(got, "transport=stdio") || !strings.Contains(got, "transport=sse") {
		t.Error("missing transport")
	}
	if !strings.Contains(got, "target=http://localhost:8080") {
		t.Error("missing target")
	}
	if !strings.Contains(got, "env=2") {
		t.Error("missing env count")
	}
}

func TestRenderMCPServerDetail_Empty(t *testing.T) {
	got := RenderMCPServerDetail(nil)
	if got != "No MCP server details available.\n" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderMCPServerDetail_Full(t *testing.T) {
	servers := []MCPServerConfigView{
		{
			Name:       "mcp1",
			Source:     MCPConfigSourceGlobal,
			Path:       "/home/.config/mcp.yaml",
			Transport:  "stdio",
			Enabled:    true,
			Effective:  true,
			Status:     "ok",
			Trust:      "trusted",
			Command:    "node",
			Args:       []string{"server.js", "--port", "9000"},
			ToolPrefix: "mcp1_",
		},
		{
			Name:      "mcp2",
			Source:    MCPConfigSourceProject,
			Transport: "sse",
			URL:       "http://localhost:8080/sse",
			HasEnv:    true,
			EnvKeys:   []string{"API_KEY"},
		},
	}
	got := RenderMCPServerDetail(servers)
	for _, want := range []string{
		"MCP server: mcp1", "source:     global", "transport:  stdio", "command:    node",
		"args:       server.js --port 9000", "tool_prefix:mcp1_",
		"MCP server: mcp2", "url:        http://localhost:8080/sse", "env_keys:   API_KEY",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "********"},
		{"short", "********"},
		{"12345678", "********"},
		{"123456789", "1234*6789"},
		{"abcdefghijklmnop", "abcd********mnop"},
	}
	for _, tc := range cases {
		got := maskKey(tc.input)
		if got != tc.want {
			t.Errorf("maskKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveMCPScope(t *testing.T) {
	cases := []struct {
		input   string
		want    MCPConfigSource
		wantErr bool
	}{
		{"", "", false},
		{"global", MCPConfigSourceGlobal, false},
		{"project", MCPConfigSourceProject, false},
		{"GLOBAL", MCPConfigSourceGlobal, false},
		{"PROJECT", MCPConfigSourceProject, false},
		{"unknown", "", true},
	}
	for _, tc := range cases {
		got, err := resolveMCPScope(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("resolveMCPScope(%q): expected error", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("resolveMCPScope(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("resolveMCPScope(%q) = %q, want %q", tc.input, got, tc.want)
			}
		}
	}
}
