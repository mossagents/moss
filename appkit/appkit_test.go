package appkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	rt "github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
)

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

func TestCommonFlags_MergeGlobalConfig_LegacyAPITypeAndName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config.SetAppName("mosscode")
	t.Cleanup(func() { config.SetAppName("moss") })
	if err := os.MkdirAll(config.AppDir(), 0700); err != nil {
		t.Fatalf("prepare config dir: %v", err)
	}
	if err := os.WriteFile(config.DefaultGlobalConfigPath(), []byte("api_type: openai\nname: deepseek\nmodel: deepseek-chat\n"), 0600); err != nil {
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

func TestCommonFlags_MergeEnv_LegacyAPITypeAndName(t *testing.T) {
	t.Setenv("MOSS_API_TYPE", "openai")
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
	if f.Trust != "trusted" {
		t.Fatalf("Trust = %q, want trusted", f.Trust)
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

func TestBuildKernelWithConfig_DefaultLLMRetry(t *testing.T) {
	k, err := BuildKernelWithConfig(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, nil, BuildConfig{
		DefaultLLMRetry: &retry.Config{
			MaxRetries:   4,
			InitialDelay: 5 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("BuildKernelWithConfig: %v", err)
	}

	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	llmRetry := loopCfg.FieldByName("LLMRetry")

	if llmRetry.FieldByName("MaxRetries").Int() != 4 {
		t.Fatalf("MaxRetries = %d, want 4", llmRetry.FieldByName("MaxRetries").Int())
	}
	if time.Duration(llmRetry.FieldByName("InitialDelay").Int()) != 5*time.Millisecond {
		t.Fatalf("InitialDelay = %v, want %v", time.Duration(llmRetry.FieldByName("InitialDelay").Int()), 5*time.Millisecond)
	}
}

func TestBuildKernelWithExtensions_AppliesOptionsAndInstallers(t *testing.T) {
	k, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, nil,
		WithKernelOptions(kernel.WithParallelToolCalls()),
		AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
			return k.ToolRegistry().Register(tool.ToolSpec{
				Name:        "test_extension_tool",
				Description: "test tool",
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"ok":true}`), nil
			})
		}),
	)
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions: %v", err)
	}

	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	if !loopCfg.FieldByName("ParallelToolCall").Bool() {
		t.Fatal("expected ParallelToolCall to be enabled by extension options")
	}

	tools := k.ToolRegistry().List()
	found := false
	for _, spec := range tools {
		if spec.Name == "test_extension_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected test_extension_tool to be registered by extension installer")
	}
}

func TestBuildKernelWithExtensions_WithScheduling(t *testing.T) {
	k, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, nil, WithScheduling(scheduler.New()))
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool, len(tools))
	for _, spec := range tools {
		toolNames[spec.Name] = true
	}
	for _, name := range []string{"schedule_task", "list_schedules", "cancel_schedule"} {
		if !toolNames[name] {
			t.Fatalf("expected scheduling tool %q to be registered", name)
		}
	}
}

func TestBuildKernelWithExtensions_WithPersistentMemories(t *testing.T) {
	memDir := filepath.Join(t.TempDir(), "memories")
	k, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, nil, WithPersistentMemories(memDir))
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(context.Background())
	})

	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool, len(tools))
	for _, spec := range tools {
		toolNames[spec.Name] = true
	}
	for _, name := range []string{"read_memory", "write_memory", "list_memories", "delete_memory"} {
		if !toolNames[name] {
			t.Fatalf("expected memory tool %q to be registered", name)
		}
	}
}

func TestBuildKernelWithExtensions_WithContextOffload(t *testing.T) {
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	k, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: ".",
	}, nil, WithContextOffload(store))
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions: %v", err)
	}

	if _, _, ok := k.ToolRegistry().Get("offload_context"); !ok {
		t.Fatal("expected offload_context tool to be registered")
	}
}

func TestBuildKernelWithExtensions_TrustGatesProjectSkillsAndAgents(t *testing.T) {
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

	restricted, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: workspace,
		Trust:     "restricted",
	}, &port.NoOpIO{})
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions restricted: %v", err)
	}
	if _, ok := rt.SkillsManager(restricted).Get("project-skill"); ok {
		t.Fatal("restricted trust should not load project skill")
	}
	if _, ok := rt.AgentRegistry(restricted).Get("project-agent"); ok {
		t.Fatal("restricted trust should not load project agent")
	}

	trusted, err := BuildKernelWithExtensions(context.Background(), &AppFlags{
		Provider:  "openai",
		Workspace: workspace,
		Trust:     "trusted",
	}, &port.NoOpIO{})
	if err != nil {
		t.Fatalf("BuildKernelWithExtensions trusted: %v", err)
	}
	if _, ok := rt.SkillsManager(trusted).Get("project-skill"); !ok {
		t.Fatal("trusted workspace should load project skill")
	}
	if _, ok := rt.AgentRegistry(trusted).Get("project-agent"); !ok {
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
