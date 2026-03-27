package defaults

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/extensions/skillsx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	kt "github.com/mossagents/moss/testing"
)

func TestSetup(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}
	sb := kt.NewMemorySandbox()

	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
		kernel.WithSandbox(sb),
	)

	ctx := context.Background()
	if err := Setup(ctx, k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	skills := skillsx.Manager(k).List()
	found := false
	for _, s := range skills {
		if s.Name == "core" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected core skill to be registered")
	}

	tools := k.ToolRegistry().List()
	if len(tools) == 0 {
		t.Error("expected tools to be registered")
	}
	toolNames := make(map[string]bool)
	for _, ts := range tools {
		toolNames[ts.Name] = true
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "ls", "grep", "run_command", "ask_user"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestSetup_WithoutBuiltin(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}

	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
	)

	ctx := context.Background()
	if err := Setup(ctx, k, ".", WithoutBuiltin()); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	skills := skillsx.Manager(k).List()
	for _, s := range skills {
		if s.Name == "core" {
			t.Error("core skill should not be registered when WithoutBuiltin is used")
		}
	}
}

func TestSetup_NoSandbox(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}

	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
	)

	ctx := context.Background()
	if err := Setup(ctx, k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool)
	for _, ts := range tools {
		toolNames[ts.Name] = true
	}
	if !toolNames["ask_user"] {
		t.Error("expected ask_user to be registered without sandbox")
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "ls", "grep", "run_command"} {
		if toolNames[name] {
			t.Errorf("tool %q should not be registered without sandbox", name)
		}
	}
}

func TestSetup_WithProgressiveSkills(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}
	sb := kt.NewMemorySandbox()

	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
		kernel.WithSandbox(sb),
	)

	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".agent", "skills", "demo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: demo
description: progressive demo
---
Demo body.
`), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := Setup(ctx, k, workspace, WithoutMCPServers(), WithProgressiveSkills()); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if _, ok := skillsx.Manager(k).Get("demo"); ok {
		t.Fatal("demo skill should not be eagerly loaded in progressive mode")
	}

	_, listHandler, ok := k.ToolRegistry().Get("list_skills")
	if !ok {
		t.Fatal("expected list_skills to be registered")
	}
	raw, err := listHandler(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list_skills: %v", err)
	}
	var listed []map[string]any
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	foundDemo := false
	for _, item := range listed {
		if item["name"] == "demo" {
			foundDemo = true
			break
		}
	}
	if !foundDemo {
		t.Fatalf("unexpected listed skills: %+v", listed)
	}

	if _, _, ok := k.ToolRegistry().Get("activate_skill"); !ok {
		t.Fatal("expected activate_skill to be registered")
	}
}

