package permissions

import (
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/collaboration"
	runtimepolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel/tool"
)

func TestCompileBuildsPolicyAndBaselineCapabilities(t *testing.T) {
	compiled, err := Compile(Profile{
		Name:                 "workspace-write",
		ApprovalPolicy:       "confirm",
		WorkspaceWriteAccess: runtimepolicy.ToolAccessAllow,
		MemoryWriteAccess:    runtimepolicy.ToolAccessRequireApproval,
		GraphMutationAccess:  runtimepolicy.ToolAccessAllow,
		AllowAsyncTasks:      true,
	}, appconfig.TrustTrusted)
	if err != nil {
		fatalf(t, "Compile() error = %v", err)
	}
	if !compiled.BaselineCapabilities.Has(collaboration.CapabilityMutateWorkspace) {
		fatalf(t, "expected mutate_workspace capability")
	}
	if !compiled.BaselineCapabilities.Has(collaboration.CapabilityLoadTrustedWorkspaceAssets) {
		fatalf(t, "expected load_trusted_workspace_assets capability")
	}
	if !compiled.BaselineCapabilities.Has(collaboration.CapabilityCreateAsyncTasks) {
		fatalf(t, "expected create_async_tasks capability")
	}
	if compiled.Policy.ApprovalMode != "confirm" {
		fatalf(t, "policy approval mode = %q, want confirm", compiled.Policy.ApprovalMode)
	}
	if compiled.Policy.MemoryWriteAccess != runtimepolicy.ToolAccessRequireApproval {
		fatalf(t, "memory write access = %q, want require-approval", compiled.Policy.MemoryWriteAccess)
	}
	planCaps := compiled.EffectiveCapabilitiesForMode(collaboration.ModePlan)
	if planCaps.Has(collaboration.CapabilityMutateWorkspace) {
		fatalf(t, "plan mode should remove mutate_workspace capability")
	}
}

func TestDeriveRequiredCapabilitiesUsesToolMetadata(t *testing.T) {
	required, err := DeriveRequiredCapabilities(tool.ToolSpec{
		Name:         "write_file",
		Capabilities: []string{"filesystem"},
		Effects:      []tool.Effect{tool.EffectWritesWorkspace},
	})
	if err != nil {
		fatalf(t, "DeriveRequiredCapabilities() error = %v", err)
	}
	if !required.Has(collaboration.CapabilityReadWorkspace) {
		fatalf(t, "expected read_workspace capability")
	}
	if !required.Has(collaboration.CapabilityMutateWorkspace) {
		fatalf(t, "expected mutate_workspace capability")
	}
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
