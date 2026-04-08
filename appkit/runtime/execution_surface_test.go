package runtime

import (
	"testing"
)

func TestProbeExecutionSurfaceReady(t *testing.T) {
	workspace := t.TempDir()
	isolationRoot := t.TempDir()

	surface := ProbeExecutionSurface(workspace, isolationRoot, true)
	statuses := surface.CapabilityStatuses()

	want := map[string]string{
		CapabilityExecutionWorkspace:      "ready",
		CapabilityExecutionExecutor:       "ready",
		CapabilityExecutionIsolation:      "ready",
		CapabilityExecutionRepoState:      "ready",
		CapabilityExecutionPatchApply:     "ready",
		CapabilityExecutionPatchRevert:    "ready",
		CapabilityExecutionWorktreeStates: "ready",
	}
	for _, status := range statuses {
		if wantState, ok := want[status.Capability]; ok {
			if status.State != wantState {
				t.Fatalf("%s state=%q, want %q", status.Capability, status.State, wantState)
			}
			delete(want, status.Capability)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing capability statuses: %v", want)
	}
}

func TestNewExecutionSurfaceDisablesIsolation(t *testing.T) {
	surface := NewExecutionSurface(t.TempDir(), "", false)
	for _, status := range surface.CapabilityStatuses() {
		if status.Capability == CapabilityExecutionIsolation {
			if status.State != "disabled" {
				t.Fatalf("workspace isolation state=%q, want disabled", status.State)
			}
			return
		}
	}
	t.Fatal("missing workspace isolation capability")
}
