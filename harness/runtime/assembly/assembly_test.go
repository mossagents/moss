package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/agent"
	"github.com/mossagents/moss/harness/capability"
	appruntime "github.com/mossagents/moss/harness/runtime"
	"github.com/mossagents/moss/harness/runtime/policy"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/guardian"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
)

type captureReporter struct {
	events []string
}

func (c *captureReporter) Report(_ context.Context, capability string, critical bool, state string, err error) {
	suffix := ""
	if err != nil {
		suffix = ":" + err.Error()
	}
	c.events = append(c.events, fmt.Sprintf("%s|%t|%s%s", capability, critical, state, suffix))
}

func defaultAssemblyConfig() Config {
	return DefaultConfig()
}

func TestResolveConfig_ConflictSkillsAndProgressive(t *testing.T) {
	cfg := defaultAssemblyConfig()
	cfg.Skills = false
	cfg.ProgressiveSkills = true
	_, err := ResolveConfig(cfg)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestInstall_UsesDefaultsParity(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	if err := Install(context.Background(), k, ".", defaultAssemblyConfig()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if manager, ok := appruntime.LookupCapabilityManager(k); !ok || manager == nil {
		t.Fatal("expected capability manager")
	}
}

func TestInstall_DefaultToolPolicyIsRestrictedConfirm(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	if err := Install(context.Background(), k, ".", defaultAssemblyConfig()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	policy, ok := policy.Current(k)
	if !ok {
		t.Fatal("expected installed tool policy")
	}
	if policy.Trust != appconfig.TrustRestricted {
		t.Fatalf("policy trust = %q, want %q", policy.Trust, appconfig.TrustRestricted)
	}
	if policy.ApprovalMode != "confirm" {
		t.Fatalf("policy approval = %q, want confirm", policy.ApprovalMode)
	}
	if !policy.Command.ClearEnv {
		t.Fatal("expected command env to be cleared by default")
	}
}

func TestInstall_GuardianDisabledByDefault(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	if err := Install(context.Background(), k, ".", defaultAssemblyConfig()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, ok := guardian.Lookup(k); ok {
		t.Fatal("guardian should be disabled by default")
	}
}

func TestInstall_GuardianEnabledFromGlobalConfig(t *testing.T) {
	orig := appconfig.AppName()
	t.Cleanup(func() { appconfig.SetAppName(orig) })
	appconfig.SetAppName("moss-assembly-guardian")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)

	globalPath := appconfig.DefaultGlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(global): %v", err)
	}
	if err := os.WriteFile(globalPath, []byte("guardian:\n  enabled: true\n  model: reviewer-mini\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(global): %v", err)
	}

	rootLLM := &kt.MockLLM{}
	k := kernel.New(
		kernel.WithLLM(rootLLM),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	if err := Install(context.Background(), k, t.TempDir(), defaultAssemblyConfig()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	g, ok := guardian.Lookup(k)
	if !ok || g == nil {
		t.Fatal("expected guardian to be installed")
	}
	if g.LLM != rootLLM {
		t.Fatal("expected guardian to reuse kernel llm by default")
	}
	if g.ModelConfig.Model != "reviewer-mini" {
		t.Fatalf("guardian model = %q, want reviewer-mini", g.ModelConfig.Model)
	}
	if g.ModelConfig.Temperature != 0 {
		t.Fatalf("guardian temperature = %v, want 0", g.ModelConfig.Temperature)
	}
}

func TestInstall_GuardianProjectConfigIgnoredForRestrictedTrust(t *testing.T) {
	orig := appconfig.AppName()
	t.Cleanup(func() { appconfig.SetAppName(orig) })
	appconfig.SetAppName("moss-assembly-guardian")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)

	workspace := t.TempDir()
	projectPath := appconfig.DefaultProjectConfigPath(workspace)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(project): %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("guardian:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(project): %v", err)
	}

	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	cfg := defaultAssemblyConfig()
	cfg.Trust = appconfig.TrustRestricted
	if err := Install(context.Background(), k, workspace, cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, ok := guardian.Lookup(k); ok {
		t.Fatal("restricted workspace should not install project guardian config")
	}
}

func TestInstall_GuardianDedicatedConfigRequiresProvider(t *testing.T) {
	orig := appconfig.AppName()
	t.Cleanup(func() { appconfig.SetAppName(orig) })
	appconfig.SetAppName("moss-assembly-guardian")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)

	globalPath := appconfig.DefaultGlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(global): %v", err)
	}
	if err := os.WriteFile(globalPath, []byte("guardian:\n  enabled: true\n  base_url: https://example.test/v1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(global): %v", err)
	}

	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	err := Install(context.Background(), k, t.TempDir(), defaultAssemblyConfig())
	if err == nil {
		t.Fatal("expected guardian config error")
	}
	if !strings.Contains(err.Error(), "guardian provider is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstall_ManagerReportsValidateReady(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	reporter := &captureReporter{}
	cfg := defaultAssemblyConfig()
	cfg.CapabilityReporter = reporter
	if err := Install(context.Background(), k, ".", cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	foundValidate := false
	foundActivate := false
	for _, ev := range reporter.events {
		if strings.HasPrefix(ev, "runtime-validate|true|ready") {
			foundValidate = true
		}
		if strings.HasPrefix(ev, "runtime-activate|true|ready") {
			foundActivate = true
		}
	}
	if !foundValidate {
		t.Fatalf("expected runtime-validate ready event, got %v", reporter.events)
	}
	if !foundActivate {
		t.Fatalf("expected runtime-activate ready event, got %v", reporter.events)
	}
}

func TestInstall_PersistsCapabilitySnapshot(t *testing.T) {
	appconfig.SetAppName("moss-runtime-test")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	if err := Install(context.Background(), k, ".", defaultAssemblyConfig()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	snapshot, err := capability.LoadCapabilitySnapshot(capability.CapabilityStatusPath())
	if err != nil {
		t.Fatalf("LoadCapabilitySnapshot: %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("expected persisted capability items")
	}
	foundBuiltin := false
	for _, item := range snapshot.Items {
		if item.Capability == "builtin-tools" && item.State == "ready" {
			foundBuiltin = true
			break
		}
	}
	if !foundBuiltin {
		t.Fatalf("expected builtin-tools ready in snapshot, got %+v", snapshot.Items)
	}
}

func TestInstall_ReportsBuiltinCriticalFailure(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	_ = k.ToolRegistry().Register(tool.NewRawTool(toolSpecNoop("read_file"), toolHandlerNoop))
	reporter := &captureReporter{}
	cfg := defaultAssemblyConfig()
	cfg.CapabilityReporter = reporter
	err := Install(context.Background(), k, ".", cfg)
	if err == nil {
		t.Fatal("expected setup error when builtin tools registration conflicts")
	}
	found := false
	for _, ev := range reporter.events {
		if strings.HasPrefix(ev, "builtin-tools|true|failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected builtin-tools critical failure report, got %v", reporter.events)
	}
}

func TestInstall_ReportsDegradedOnOptionalSkillParseFailure(t *testing.T) {
	ws := t.TempDir()
	skillDir := filepath.Join(ws, ".agents", "skills", "broken-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: builtin-tools\n---\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}

	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	reporter := &captureReporter{}
	cfg := defaultAssemblyConfig()
	cfg.CapabilityReporter = reporter
	cfg.Trust = appconfig.TrustTrusted
	if err := Install(context.Background(), k, ws, cfg); err != nil {
		t.Fatalf("install should not fail on optional skill parse failure: %v", err)
	}
	found := false
	for _, ev := range reporter.events {
		if strings.Contains(ev, "degraded") && strings.Contains(ev, "skill:builtin-tools") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected degraded skill report, got %v", reporter.events)
	}
}

func TestInstallAgents_TrustedWorkspaceLoadsProjectAgentAndReportsReady(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	reporter := &captureReporter{}
	cfg := defaultAssemblyConfig()
	cfg.CapabilityReporter = reporter
	cfg.Trust = appconfig.TrustTrusted
	if err := Install(context.Background(), k, workspace, cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, ok := agent.KernelRegistry(k).Get("project-agent"); !ok {
		t.Fatal("expected trusted workspace agent to be loaded")
	}

	workspaceCapability := "agents:" + filepath.Join(workspace, ".agents", "agents")
	if !containsReportPrefix(reporter.events, workspaceCapability+"|false|ready") {
		t.Fatalf("expected ready report for %s, got %v", workspaceCapability, reporter.events)
	}
	if !containsReportPrefix(reporter.events, "subagent:project-agent|false|ready") {
		t.Fatalf("expected subagent ready report, got %v", reporter.events)
	}
}

func TestInstallAgents_RestrictedWorkspaceSkipsProjectAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	reporter := &captureReporter{}
	cfg := defaultAssemblyConfig()
	cfg.CapabilityReporter = reporter
	cfg.Trust = appconfig.TrustRestricted
	if err := Install(context.Background(), k, workspace, cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, ok := agent.KernelRegistry(k).Get("project-agent"); ok {
		t.Fatal("restricted workspace should not load project agent")
	}

	workspaceCapability := "agents:" + filepath.Join(workspace, ".agents", "agents")
	if containsReportPrefix(reporter.events, workspaceCapability+"|false|ready") || containsReportPrefix(reporter.events, workspaceCapability+"|false|degraded") {
		t.Fatalf("restricted workspace should not report project agent dir, got %v", reporter.events)
	}
}

func TestInstallAgents_RuntimeDiscoveredAgentIsDelegatableAfterBoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	cfg := defaultAssemblyConfig()
	cfg.Trust = appconfig.TrustTrusted
	if err := Install(context.Background(), k, workspace, cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	delegateTool, ok := k.ToolRegistry().Get("delegate_agent")
	if !ok {
		t.Fatal("expected delegate_agent tool after boot")
	}
	input, err := json.Marshal(map[string]any{
		"agent": "project-agent",
		"task":  "confirm runtime-discovered agent is delegatable",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	output, err := delegateTool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("delegate_agent.Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp["status"] != "completed" {
		t.Fatalf("status = %v, want completed", resp["status"])
	}
	if resp["agent"] != "project-agent" {
		t.Fatalf("agent = %v, want project-agent", resp["agent"])
	}
}

func TestCollectAgentDirs_trusted_includesWorkspaceFirst(t *testing.T) {
	ws := t.TempDir()
	cfg := Config{Trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(ws, cfg)

	if len(dirs) == 0 {
		t.Fatal("expected at least one dir")
	}
	want := filepath.Join(ws, ".agents", "agents")
	if dirs[0] != want {
		t.Errorf("dirs[0]: want %q got %q", want, dirs[0])
	}
}

func TestCollectAgentDirs_trusted_alsoIncludesHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	cfg := Config{Trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(t.TempDir(), cfg)

	wantSuffix := filepath.Join(".moss", "agents")
	for _, d := range dirs {
		if strings.HasSuffix(d, wantSuffix) {
			return
		}
	}
	t.Errorf("home dir entry (~/.moss/agents) not found in %v; home=%s", dirs, home)
}

func TestCollectAgentDirs_restricted_excludesWorkspace(t *testing.T) {
	ws := t.TempDir()
	cfg := Config{Trust: appconfig.TrustRestricted}

	dirs := collectAgentDirs(ws, cfg)

	wsPrefix := filepath.Join(ws, ".agents")
	for _, d := range dirs {
		if strings.HasPrefix(d, wsPrefix) {
			t.Errorf("restricted trust should not include workspace agents dir, but got %q", d)
		}
	}
}

func TestCollectAgentDirs_restricted_includesHomeDir(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home dir available")
	}
	cfg := Config{Trust: appconfig.TrustRestricted}

	dirs := collectAgentDirs(t.TempDir(), cfg)

	wantSuffix := filepath.Join(".moss", "agents")
	for _, d := range dirs {
		if strings.HasSuffix(d, wantSuffix) {
			return
		}
	}
	t.Errorf("~/.moss/agents not found in dirs: %v", dirs)
}

func TestCollectAgentDirs_emptyTrust_treatedAsTrusted(t *testing.T) {
	ws := t.TempDir()
	cfg := Config{Trust: ""}

	dirs := collectAgentDirs(ws, cfg)

	want := filepath.Join(ws, ".agents", "agents")
	if len(dirs) == 0 || dirs[0] != want {
		t.Errorf("dirs[0]: want %q got %v", want, dirs)
	}
}

func TestCollectAgentDirs_order_workspaceBeforeHome(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home dir available")
	}
	ws := t.TempDir()
	cfg := Config{Trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(ws, cfg)

	if len(dirs) < 2 {
		t.Fatalf("expected at least 2 dirs, got %v", dirs)
	}
	wantFirst := filepath.Join(ws, ".agents", "agents")
	if dirs[0] != wantFirst {
		t.Errorf("workspace dir should be first: want %q, got %q", wantFirst, dirs[0])
	}
	wantSuffix := filepath.Join(".moss", "agents")
	if !strings.HasSuffix(dirs[1], wantSuffix) {
		t.Errorf("home dir should be second: want suffix %q, got %q", wantSuffix, dirs[1])
	}
}

func toolSpecNoop(name string) tool.ToolSpec {
	return tool.ToolSpec{Name: name}
}

func toolHandlerNoop(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

func newRuntimeAgentsTestKernel() *kernel.Kernel {
	return kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
}

func writeTestAgent(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name+".yaml")
	data := []byte(`
name: "` + name + `"
description: "Project agent"
system_prompt: "Project agent prompt."
tools: []
trust_level: restricted
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func containsReportPrefix(events []string, prefix string) bool {
	for _, ev := range events {
		if strings.HasPrefix(ev, prefix) {
			return true
		}
	}
	return false
}
