package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	kt "github.com/mossagents/moss/testing"
)

func TestSetupAgents_TrustedWorkspaceLoadsProjectAgentAndReportsReady(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	reporter := &captureReporter{}
	if err := Setup(context.Background(), k, workspace, WithCapabilityReporter(reporter), WithWorkspaceTrust(appconfig.TrustTrusted)); err != nil {
		t.Fatalf("Setup: %v", err)
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

func TestSetupAgents_RestrictedWorkspaceSkipsProjectAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	reporter := &captureReporter{}
	if err := Setup(context.Background(), k, workspace, WithCapabilityReporter(reporter), WithWorkspaceTrust(appconfig.TrustRestricted)); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if _, ok := agent.KernelRegistry(k).Get("project-agent"); ok {
		t.Fatal("restricted workspace should not load project agent")
	}

	workspaceCapability := "agents:" + filepath.Join(workspace, ".agents", "agents")
	if containsReportPrefix(reporter.events, workspaceCapability+"|false|ready") || containsReportPrefix(reporter.events, workspaceCapability+"|false|degraded") {
		t.Fatalf("restricted workspace should not report project agent dir, got %v", reporter.events)
	}
}

func TestSetupAgents_RuntimeDiscoveredAgentIsDelegatableAfterBoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()
	writeTestAgent(t, filepath.Join(workspace, ".agents", "agents"), "project-agent")

	k := newRuntimeAgentsTestKernel()
	if err := Setup(context.Background(), k, workspace, WithWorkspaceTrust(appconfig.TrustTrusted)); err != nil {
		t.Fatalf("Setup: %v", err)
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

func newRuntimeAgentsTestKernel() *kernel.Kernel {
	return kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
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
