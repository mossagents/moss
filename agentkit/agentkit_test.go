package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	appconfig "github.com/mossagents/moss/kernel/config"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/scheduler"
	"github.com/mossagents/moss/kernel/tool"
)

func TestDefaultTemplateContext(t *testing.T) {
	ctx := appconfig.DefaultTemplateContext("/workspace")

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
	// Test with a new AppFlags - should have defaults
	f := &AppFlags{
		Provider: "openai",
	}
	f.MergeGlobalConfig()

	// Provider should remain "openai" since it was explicitly set
	if f.Provider != "openai" {
		t.Errorf("Provider = %v, want openai", f.Provider)
	}
}

func TestCommonFlags_MergeEnv(t *testing.T) {
	t.Setenv("MOSS_PROVIDER", "claude")
	t.Setenv("MOSS_MODEL", "claude-sonnet")

	f := &AppFlags{}
	f.MergeEnv("MOSS")

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

func TestRenderSystemPrompt(t *testing.T) {
	ctx := appconfig.DefaultTemplateContext("/workspace")
	ctx["Capital"] = 123
	prompt := appconfig.RenderSystemPrompt("/workspace", `OS={{.OS}} Workspace={{.Workspace}} Capital={{.Capital}}`, ctx)
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
