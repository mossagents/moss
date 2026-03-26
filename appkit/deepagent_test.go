package appkit

import (
	"context"
	"testing"

	"github.com/mossagents/moss/extensions/agentsx"
	"github.com/mossagents/moss/kernel/port"
)

func TestBuildDeepAgentKernel_DefaultPreset(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}

	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := map[string]bool{}
	for _, spec := range tools {
		toolNames[spec.Name] = true
	}
	for _, name := range []string{
		"read_file", "write_file", "edit_file", "glob", "list_files", "search_text", "run_command", "ask_user",
	} {
		if !toolNames[name] {
			t.Fatalf("expected built-in tool %q", name)
		}
	}

	reg := agentsx.Registry(k)
	gp, ok := reg.Get("general-purpose")
	if !ok {
		t.Fatal("expected general-purpose agent preset")
	}
	if gp.TrustLevel != "restricted" {
		t.Fatalf("general-purpose trust=%q, want restricted", gp.TrustLevel)
	}
	if gp.MaxSteps <= 0 {
		t.Fatalf("general-purpose max_steps=%d, want >0", gp.MaxSteps)
	}
	if len(gp.Tools) == 0 {
		t.Fatal("expected general-purpose tools to be populated")
	}
}

func TestBuildDeepAgentKernel_DisableGeneralPurpose(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}

	disable := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnsureGeneralPurpose: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}

	if _, ok := agentsx.Registry(k).Get("general-purpose"); ok {
		t.Fatal("general-purpose should not be auto-created when disabled")
	}
}
