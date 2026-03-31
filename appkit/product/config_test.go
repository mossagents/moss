package product

import (
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/mossagents/moss/config"
)

func TestApplyConfigSetAndUnset(t *testing.T) {
	cfg := &appconfig.Config{}
	display, err := applyConfigSet(cfg, "provider", "openai", false)
	if err != nil {
		t.Fatalf("set provider: %v", err)
	}
	if display != "openai" {
		t.Fatalf("expected provider display openai, got %q", display)
	}
	if cfg.EffectiveAPIType() != "openai" {
		t.Fatalf("expected api_type=openai, got %q", cfg.EffectiveAPIType())
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
	if len(restricted) != 4 {
		t.Fatalf("restricted mcp count = %d, want 4", len(restricted))
	}
	assertMCPStatus(t, restricted, "project-mcp", MCPConfigSourceProject, "suppressed_by_trust", false)
	assertMCPStatus(t, restricted, "global-mcp", MCPConfigSourceGlobal, "enabled", true)

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
