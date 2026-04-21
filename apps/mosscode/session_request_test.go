package main

import (
	"strings"
	"testing"

	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
)

func TestResolveRuntimeInvocationUsesTypedRequest(t *testing.T) {
	initTestApp(t)
	cfg := &config{
		flags: &appkit.AppFlags{
			Workspace: t.TempDir(),
			Provider:  "openai",
			Model:     "gpt-5",
			Trust:     "restricted",
		},
		request: sessionRequest{
			Preset:            "code",
			CollaborationMode: "plan",
		},
		governance: product.DefaultGovernanceConfig(),
	}

	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		t.Fatalf("resolveRuntimeInvocation: %v", err)
	}
	if got := invocation.ResolvedSpec.Intent.CollaborationMode; got != "plan" {
		t.Fatalf("collaboration mode = %q, want plan", got)
	}
	if got := invocation.ResolvedSpec.Runtime.PermissionProfile; got != "workspace-write" {
		t.Fatalf("permission profile = %q, want workspace-write", got)
	}
	if got := invocation.ResolvedSpec.Origin.Preset; got != "code" {
		t.Fatalf("preset = %q, want code", got)
	}
	if got := invocation.ApprovalMode; got != "confirm" {
		t.Fatalf("approval mode = %q, want confirm", got)
	}
}

func TestResolveRuntimeInvocationDefaultsToTypedPreset(t *testing.T) {
	initTestApp(t)
	cfg := &config{
		flags: &appkit.AppFlags{
			Workspace: t.TempDir(),
			Provider:  "openai",
			Model:     "gpt-5",
			Trust:     "restricted",
		},
		governance: product.DefaultGovernanceConfig(),
	}

	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		t.Fatalf("resolveRuntimeInvocation: %v", err)
	}
	if got := invocation.ResolvedSpec.Origin.Preset; got != "code" {
		t.Fatalf("preset = %q, want code", got)
	}
	if got := invocation.ResolvedSpec.Intent.CollaborationMode; got != "execute" {
		t.Fatalf("collaboration mode = %q, want execute", got)
	}
	if got := invocation.ResolvedSpec.Runtime.PermissionProfile; got != "workspace-write" {
		t.Fatalf("permission profile = %q, want workspace-write", got)
	}
}

func TestRunDebugConfigShowsTypedSelectors(t *testing.T) {
	initTestApp(t)
	cfg := &config{
		flags: &appkit.AppFlags{
			Workspace: t.TempDir(),
			Provider:  "openai",
			Model:     "gpt-5",
			Trust:     "restricted",
		},
		request: sessionRequest{
			Preset:            "code",
			CollaborationMode: "investigate",
			PermissionProfile: "read-only",
		},
		governance: product.DefaultGovernanceConfig(),
	}

	out, err := captureStdout(func() error { return runDebugConfig(cfg) })
	if err != nil {
		t.Fatalf("runDebugConfig: %v", err)
	}
	for _, want := range []string{
		"Session selectors: run=interactive | preset=code | mode=investigate | permissions=read-only",
		"mosscode debug-config",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %s", want, out)
		}
	}
}
