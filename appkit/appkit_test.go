package appkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/mossagents/moss/config"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	rt "github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/scheduler"
)

type appkitTestWorkspace struct{}

func (appkitTestWorkspace) ReadFile(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (appkitTestWorkspace) WriteFile(_ context.Context, _ string, _ []byte) error   { return nil }
func (appkitTestWorkspace) ListFiles(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (appkitTestWorkspace) Stat(_ context.Context, _ string) (workspace.FileInfo, error) {
	return workspace.FileInfo{}, nil
}
func (appkitTestWorkspace) DeleteFile(_ context.Context, _ string) error { return nil }

type appkitTestExecutor struct{}

func (appkitTestExecutor) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{}, nil
}

func TestDefaultTemplateContext(t *testing.T) {
	ctx := config.DefaultTemplateContext("/workspace")

	if ctx["OS"] != runtime.GOOS {
		t.Errorf("OS = %v, want %v", ctx["OS"], runtime.GOOS)
	}
	if ctx["Arch"] != runtime.GOARCH {
		t.Errorf("Arch = %v, want %v", ctx["Arch"], runtime.GOARCH)
	}
	if ctx["Workspace"] != "/workspace" {
		t.Errorf("Workspace = %v, want /workspace", ctx["Workspace"])
	}

	if runtime.GOOS == "windows" {
		if ctx["Shell"] != "powershell" {
			t.Errorf("Shell = %v, want powershell on windows", ctx["Shell"])
		}
	} else {
		if ctx["Shell"] != "bash" {
			t.Errorf("Shell = %v, want bash on non-windows", ctx["Shell"])
		}
	}

	if _, ok := ctx["Hostname"]; !ok {
		t.Error("expected Hostname key in context")
	}
}

func TestCommonFlags_MergeGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config.SetAppName("mosscode")
	t.Cleanup(func() { config.SetAppName("moss") })
	if err := os.MkdirAll(config.AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	if err := os.WriteFile(config.DefaultGlobalConfigPath(), []byte("provider: claude\nmodel: sonnet\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	f := &AppFlags{}
	f.MergeGlobalConfig()
	f.ApplyDefaults()

	if f.Provider != "claude" {
		t.Fatalf("Provider = %v, want claude", f.Provider)
	}
	if f.Model != "sonnet" {
		t.Fatalf("Model = %v, want sonnet", f.Model)
	}
}

func TestCommonFlags_MergeGlobalConfig_ProviderAndName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config.SetAppName("mosscode")
	t.Cleanup(func() { config.SetAppName("moss") })
	if err := os.MkdirAll(config.AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	if err := os.WriteFile(config.DefaultGlobalConfigPath(), []byte("provider: openai\nname: deepseek\nmodel: deepseek-chat\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	f := &AppFlags{}
	f.MergeGlobalConfig()
	f.ApplyDefaults()

	if f.Name != "deepseek" {
		t.Fatalf("Name = %q, want deepseek", f.Name)
	}
	if f.Provider != config.APITypeOpenAICompletions {
		t.Fatalf("Provider alias = %q, want %s", f.Provider, config.APITypeOpenAICompletions)
	}
}

func TestCommonFlags_MergeEnv(t *testing.T) {
	t.Setenv("MOSS_PROVIDER", "claude")
	t.Setenv("MOSS_MODEL", "claude-sonnet")

	f := &AppFlags{}
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	if f.Provider != "claude" {
		t.Fatalf("Provider = %q, want claude", f.Provider)
	}
	if f.Model != "claude-sonnet" {
		t.Fatalf("Model = %q, want claude-sonnet", f.Model)
	}
	if _, ok := os.LookupEnv("MOSS_PROVIDER"); !ok {
		t.Fatal("expected env to be set during test")
	}
}

func TestCommonFlags_MergeEnv_ProviderAndName(t *testing.T) {
	t.Setenv("MOSS_PROVIDER", "openai")
	t.Setenv("MOSS_NAME", "deepseek")
	t.Setenv("MOSS_MODEL", "deepseek-chat")

	f := &AppFlags{}
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	if f.Name != "deepseek" {
		t.Fatalf("Name = %q, want deepseek", f.Name)
	}
	if f.Provider != config.APITypeOpenAICompletions {
		t.Fatalf("Provider alias = %q, want %s", f.Provider, config.APITypeOpenAICompletions)
	}
}

func TestCommonFlags_ApplyDefaults(t *testing.T) {
	f := &AppFlags{}
	f.ApplyDefaults()
	if f.Provider != config.APITypeOpenAICompletions {
		t.Fatalf("Provider = %q, want %s", f.Provider, config.APITypeOpenAICompletions)
	}
	if f.Workspace != "." {
		t.Fatalf("Workspace = %q, want .", f.Workspace)
	}
	if f.Trust != config.TrustRestricted {
		t.Fatalf("Trust = %q, want %s", f.Trust, config.TrustRestricted)
	}
	if f.BudgetGovernance != "observe-only" {
		t.Fatalf("BudgetGovernance = %q, want observe-only", f.BudgetGovernance)
	}
}

func TestCommonFlags_MergeEnv_PromptSettings(t *testing.T) {
	t.Setenv("MOSS_PROMPT_VERSION", "p1-unified-v1")

	f := &AppFlags{}
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	if f.PromptVersion != "p1-unified-v1" {
		t.Fatalf("PromptVersion = %q, want p1-unified-v1", f.PromptVersion)
	}
}

func TestCommonFlags_MergeEnv_BudgetGovernance(t *testing.T) {
	t.Setenv("MOSS_BUDGET_GOVERNANCE", "enforce")
	t.Setenv("MOSS_GLOBAL_MAX_TOKENS", "9000")
	t.Setenv("MOSS_GLOBAL_MAX_STEPS", "120")
	t.Setenv("MOSS_GLOBAL_BUDGET_WARN_AT", "0.75")

	f := &AppFlags{}
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	if f.BudgetGovernance != "enforce" {
		t.Fatalf("BudgetGovernance = %q, want enforce", f.BudgetGovernance)
	}
	if f.GlobalMaxTokens != 9000 || f.GlobalMaxSteps != 120 {
		t.Fatalf("unexpected global limits: tokens=%d steps=%d", f.GlobalMaxTokens, f.GlobalMaxSteps)
	}
	if f.GlobalWarnAt != 0.75 {
		t.Fatalf("GlobalWarnAt = %f, want 0.75", f.GlobalWarnAt)
	}
}

func TestRenderSystemPrompt(t *testing.T) {
	ctx := config.DefaultTemplateContext("/workspace")
	ctx["Capital"] = 123
	prompt := config.RenderSystemPrompt("/workspace", `OS={{.OS}} Workspace={{.Workspace}} Capital={{.Capital}}`, ctx)
	if prompt == "" {
		t.Fatal("expected rendered prompt")
	}
	if prompt != "OS="+runtime.GOOS+" Workspace=/workspace Capital=123" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildKernelWithFeatures_AppliesOptionsAndInstallers(t *testing.T) {
	k, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, &io.NoOpIO{},
		harness.KernelOptions(kernel.WithParallelToolCalls()),
		harness.FeatureFunc{FeatureName: "test-extension-tool", InstallFunc: func(_ context.Context, h *harness.Harness) error {
			return h.Kernel().ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
				Name:        "test_extension_tool",
				Description: "test tool",
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"ok":true}`), nil
			}))
		}},
	)
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}

	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	if !loopCfg.FieldByName("ParallelToolCall").Bool() {
		t.Fatal("expected ParallelToolCall to be enabled by extension options")
	}

	tools := k.ToolRegistry().List()
	found := false
	for _, spec := range tools {
		if spec.Name() == "test_extension_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected test_extension_tool to be registered by extension installer")
	}
}

func TestBuildKernelWithFeatures_NoFeaturesMatchesBuildKernel(t *testing.T) {
	flags := isolatedBuildFlags(t)

	base, err := BuildKernel(context.Background(), flags, &io.NoOpIO{})
	if err != nil {
		t.Fatalf("BuildKernel: %v", err)
	}
	withFeatures, err := BuildKernelWithFeatures(context.Background(), flags, &io.NoOpIO{},
		harness.RuntimeSetup(flags.Workspace, flags.Trust),
	)
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}

	if !reflect.DeepEqual(toolNames(base), toolNames(withFeatures)) {
		t.Fatalf("tool sets diverged:\nbase=%v\nwithFeatures=%v", toolNames(base), toolNames(withFeatures))
	}
}

func TestBuildKernelWithFeatures_RuntimeOptionsAffectSetup(t *testing.T) {
	flags := isolatedBuildFlags(t)

	k, err := BuildKernelWithFeatures(context.Background(), flags, &io.NoOpIO{},
		harness.RuntimeSetup(flags.Workspace, flags.Trust, harness.WithBuiltinTools(false)),
	)
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("read_file"); ok {
		t.Fatal("expected builtin tools to be disabled by runtime option extension")
	}
}

func TestBuildKernelWithFeatures_GovernsFeaturePhases(t *testing.T) {
	flags := isolatedBuildFlags(t)
	runtimeSeen := false
	k, err := BuildKernelWithFeatures(context.Background(), flags, &io.NoOpIO{},
		harness.FeatureFunc{
			FeatureName: "late-check",
			MetadataValue: harness.FeatureMetadata{
				Phase: harness.FeaturePhasePostRuntime,
			},
			InstallFunc: func(_ context.Context, h *harness.Harness) error {
				_, runtimeSeen = h.Kernel().ToolRegistry().Get("read_file")
				if !runtimeSeen {
					return fmt.Errorf("expected runtime tools to be registered before post-runtime feature")
				}
				return nil
			},
		},
		harness.RuntimeSetup(flags.Workspace, flags.Trust),
	)
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}
	if k == nil {
		t.Fatal("expected kernel")
	}
	if !runtimeSeen {
		t.Fatal("expected post-runtime feature to observe runtime setup side effects")
	}
}

func TestBuildKernel_DefaultManagedBackendProvidesPorts(t *testing.T) {
	flags := isolatedBuildFlags(t)

	k, err := BuildKernel(context.Background(), flags, &io.NoOpIO{})
	if err != nil {
		t.Fatalf("BuildKernel: %v", err)
	}
	if k.Sandbox() == nil {
		t.Fatal("expected default builder to provision a sandbox-backed backend")
	}
	if k.Workspace() == nil || k.Executor() == nil {
		t.Fatal("expected default builder to provision workspace and executor ports")
	}
}

func TestBuildKernel_ExplicitPortsBypassDefaultLocalBackend(t *testing.T) {
	flags := isolatedBuildFlags(t)
	ws := appkitTestWorkspace{}
	exec := appkitTestExecutor{}

	k, err := BuildKernel(context.Background(), flags, &io.NoOpIO{},
		kernel.WithWorkspace(ws),
		kernel.WithExecutor(exec),
	)
	if err != nil {
		t.Fatalf("BuildKernel: %v", err)
	}
	if got := k.Workspace(); got != ws {
		t.Fatalf("workspace = %#v, want %#v", got, ws)
	}
	if got := k.Executor(); got != exec {
		t.Fatalf("executor = %#v, want %#v", got, exec)
	}
	if k.Sandbox() != nil {
		t.Fatal("expected explicit workspace/executor injection to bypass default local sandbox backend")
	}
}

func isolatedBuildFlags(t *testing.T) *AppFlags {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)
	return &AppFlags{
		Provider:  "openai",
		Workspace: t.TempDir(),
	}
}

func toolNames(k *kernel.Kernel) []string {
	names := make([]string, 0, len(k.ToolRegistry().List()))
	for _, spec := range k.ToolRegistry().List() {
		names = append(names, spec.Name())
	}
	sort.Strings(names)
	return names
}

func TestBuildKernelWithFeatures_WithScheduling(t *testing.T) {
	k, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, &io.NoOpIO{}, harness.RuntimeSetup(".", ""), harness.Scheduling(scheduler.New()))
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool, len(tools))
	for _, spec := range tools {
		toolNames[spec.Name()] = true
	}
	for _, name := range []string{"schedule_task", "list_schedules", "cancel_schedule"} {
		if !toolNames[name] {
			t.Fatalf("expected scheduling tool %q to be registered", name)
		}
	}
}

func TestBuildKernelWithFeatures_WithPersistentMemories(t *testing.T) {
	memDir := filepath.Join(t.TempDir(), "memories")
	k, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, &io.NoOpIO{}, harness.RuntimeSetup(".", ""), harness.PersistentMemories(memDir))
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(context.Background())
	})

	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool, len(tools))
	for _, spec := range tools {
		toolNames[spec.Name()] = true
	}
	for _, name := range []string{"read_memory", "write_memory", "list_memories", "delete_memory"} {
		if !toolNames[name] {
			t.Fatalf("expected memory tool %q to be registered", name)
		}
	}
}

func TestBuildKernelWithFeatures_WithContextOffload(t *testing.T) {
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	k, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, &io.NoOpIO{}, harness.RuntimeSetup(".", ""), harness.ContextOffload(store))
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures: %v", err)
	}

	if _, ok := k.ToolRegistry().Get("offload_context"); !ok {
		t.Fatal("expected offload_context tool to be registered")
	}
}

func TestBuildKernelWithFeatures_TrustGatesProjectSkillsAndAgents(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	skillDir := filepath.Join(workspace, ".agents", "skills", "project-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: project-skill
description: project only
---
project skill body
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	agentDir := filepath.Join(workspace, ".agents", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "project-agent.yaml"), []byte(`name: project-agent
description: "Project agent"
system_prompt: "Project agent prompt."
tools: [read_file]
trust_level: restricted
`), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	restricted, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: workspace,
		Trust:     "restricted",
	}, &io.NoOpIO{}, harness.RuntimeSetup(workspace, "restricted"))
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures restricted: %v", err)
	}
	if _, ok := rt.CapabilityManager(restricted).Get("project-skill"); ok {
		t.Fatal("restricted trust should not load project skill")
	}
	if _, ok := harness.SubagentCatalogOf(restricted).Get("project-agent"); ok {
		t.Fatal("restricted trust should not load project agent")
	}

	trusted, err := BuildKernelWithFeatures(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: workspace,
		Trust:     "trusted",
	}, &io.NoOpIO{}, harness.RuntimeSetup(workspace, "trusted"))
	if err != nil {
		t.Fatalf("BuildKernelWithFeatures trusted: %v", err)
	}
	if _, ok := rt.CapabilityManager(trusted).Get("project-skill"); !ok {
		t.Fatal("trusted workspace should load project skill")
	}
	if _, ok := harness.SubagentCatalogOf(trusted).Get("project-agent"); !ok {
		t.Fatal("trusted workspace should load project agent")
	}
}

func TestOrderedKeys(t *testing.T) {
	m := map[string]string{
		"Custom":    "value",
		"Provider":  "openai",
		"Tools":     "12",
		"Model":     "gpt-4o",
		"Workspace": ".",
	}

	keys := orderedKeys(m)
	if len(keys) != 5 {
		t.Fatalf("expected 5 keys, got %d", len(keys))
	}

	// Provider should come before Model, which should come before Workspace
	providerIdx, modelIdx, wsIdx := -1, -1, -1
	for i, k := range keys {
		switch k {
		case "Provider":
			providerIdx = i
		case "Model":
			modelIdx = i
		case "Workspace":
			wsIdx = i
		}
	}

	if providerIdx >= modelIdx {
		t.Errorf("Provider (idx %d) should come before Model (idx %d)", providerIdx, modelIdx)
	}
	if modelIdx >= wsIdx {
		t.Errorf("Model (idx %d) should come before Workspace (idx %d)", modelIdx, wsIdx)
	}
}
