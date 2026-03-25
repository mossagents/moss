package appkit

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/mossagi/moss/kernel/retry"
)

func TestDefaultTemplateContext(t *testing.T) {
	ctx := DefaultTemplateContext("/workspace")

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
	prompt := RenderSystemPrompt("/workspace", `OS={{.OS}} Workspace={{.Workspace}} Capital={{.Capital}}`, map[string]any{
		"Capital": 123,
	})
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
