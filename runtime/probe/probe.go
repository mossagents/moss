package probe

import (
	"context"
	"errors"
	extcap "github.com/mossagents/moss/extensions/capability"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"strings"
)

const (
	CapabilityExecutionWorkspace      = "execution:workspace"
	CapabilityExecutionExecutor       = "execution:executor"
	CapabilityExecutionIsolation      = "execution:workspace_isolation"
	CapabilityExecutionRepoState      = "execution:repo_state_capture"
	CapabilityExecutionPatchApply     = "execution:patch_apply"
	CapabilityExecutionPatchRevert    = "execution:patch_revert"
	CapabilityExecutionWorktreeStates = "execution:worktree_snapshots"
)

type ExecutionProbe struct {
	WorkspaceRoot    string
	IsolationRoot    string
	IsolationEnabled bool

	workspace         workspace.Workspace
	executor          workspace.Executor
	isolation         workspace.WorkspaceIsolation
	repoStateCapture  workspace.RepoStateCapture
	patchApply        workspace.PatchApply
	patchRevert       workspace.PatchRevert
	worktreeSnapshots workspace.WorktreeSnapshotStore

	errors   map[string]error
	disabled map[string]bool
}

func NewExecutionProbe(workspaceRoot, isolationRoot string, enableIsolation bool) *ExecutionProbe {
	probe := &ExecutionProbe{
		WorkspaceRoot:    strings.TrimSpace(workspaceRoot),
		IsolationRoot:    strings.TrimSpace(isolationRoot),
		IsolationEnabled: enableIsolation,
		errors:           map[string]error{},
		disabled:         map[string]bool{},
	}
	if enableIsolation {
		if probe.IsolationRoot == "" {
			probe.errors[CapabilityExecutionIsolation] = errors.New("workspace isolation root is empty")
		} else if isolation, err := sandbox.NewLocalWorkspaceIsolation(probe.IsolationRoot); err != nil {
			probe.errors[CapabilityExecutionIsolation] = err
		} else {
			probe.isolation = isolation
		}
	} else {
		probe.disabled[CapabilityExecutionIsolation] = true
	}
	probe.repoStateCapture = sandbox.NewGitRepoStateCapture(probe.WorkspaceRoot)
	probe.patchApply = sandbox.NewGitPatchApply(probe.WorkspaceRoot)
	probe.patchRevert = sandbox.NewGitPatchRevert(probe.WorkspaceRoot)
	probe.worktreeSnapshots = sandbox.NewGitWorktreeSnapshotStore(probe.WorkspaceRoot)
	return probe
}

func ProbeExecutionCapabilities(workspaceRoot, isolationRoot string, enableIsolation bool) *ExecutionProbe {
	probe := NewExecutionProbe(workspaceRoot, isolationRoot, enableIsolation)
	sb, err := sandbox.NewLocal(strings.TrimSpace(workspaceRoot))
	if err != nil {
		probe.errors[CapabilityExecutionWorkspace] = err
		probe.errors[CapabilityExecutionExecutor] = err
		return probe
	}
	probe.workspace = sandbox.NewLocalWorkspace(sb)
	probe.executor = sandbox.NewLocalExecutor(sb)
	return probe
}

func ExecutionProbeFromKernel(k *kernel.Kernel, workspaceRoot, isolationRoot string, enableIsolation bool) *ExecutionProbe {
	if k == nil {
		return ProbeExecutionCapabilities(workspaceRoot, isolationRoot, enableIsolation)
	}
	probe := &ExecutionProbe{
		WorkspaceRoot:     strings.TrimSpace(workspaceRoot),
		IsolationRoot:     strings.TrimSpace(isolationRoot),
		IsolationEnabled:  enableIsolation,
		workspace:         k.Workspace(),
		executor:          k.Executor(),
		isolation:         k.WorkspaceIsolation(),
		repoStateCapture:  k.RepoStateCapture(),
		patchApply:        k.PatchApply(),
		patchRevert:       k.PatchRevert(),
		worktreeSnapshots: k.WorktreeSnapshots(),
		errors:            map[string]error{},
		disabled:          map[string]bool{},
	}
	if !enableIsolation {
		probe.disabled[CapabilityExecutionIsolation] = true
	}
	return probe
}

func (p *ExecutionProbe) Error(capability string) error {
	if p == nil {
		return nil
	}
	return p.errors[strings.TrimSpace(capability)]
}

func (p *ExecutionProbe) CapabilityStatuses() []extcap.CapabilityStatus {
	if p == nil {
		return nil
	}
	return []extcap.CapabilityStatus{
		p.capabilityStatus(CapabilityExecutionWorkspace, "workspace", p.workspace != nil, true),
		p.capabilityStatus(CapabilityExecutionExecutor, "executor", p.executor != nil, true),
		p.capabilityStatus(CapabilityExecutionIsolation, "workspace_isolation", p.isolation != nil, false),
		p.capabilityStatus(CapabilityExecutionRepoState, "repo_state_capture", p.repoStateCapture != nil, false),
		p.capabilityStatus(CapabilityExecutionPatchApply, "patch_apply", p.patchApply != nil, false),
		p.capabilityStatus(CapabilityExecutionPatchRevert, "patch_revert", p.patchRevert != nil, false),
		p.capabilityStatus(CapabilityExecutionWorktreeStates, "worktree_snapshots", p.worktreeSnapshots != nil, false),
	}
}

func ReportExecutionProbe(ctx context.Context, reporter extcap.CapabilityReporter, probe *ExecutionProbe) {
	if reporter == nil || probe == nil {
		return
	}
	for _, status := range probe.CapabilityStatuses() {
		reporter.Report(ctx, status.Capability, status.Critical, status.State, probe.Error(status.Capability))
	}
}

func (p *ExecutionProbe) capabilityStatus(capability, name string, ready bool, critical bool) extcap.CapabilityStatus {
	status := extcap.CapabilityStatus{
		Capability: capability,
		Kind:       "execution",
		Name:       name,
		Critical:   critical,
	}
	if p.disabled[capability] {
		status.State = "disabled"
		return status
	}
	if ready {
		status.State = "ready"
		return status
	}
	if err := p.errors[capability]; err != nil {
		status.Error = err.Error()
		if critical {
			status.State = "failed"
		} else {
			status.State = "degraded"
		}
		return status
	}
	if critical {
		status.State = "failed"
		return status
	}
	status.State = "degraded"
	return status
}

