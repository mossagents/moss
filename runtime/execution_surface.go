package runtime

import (
	"context"
	"errors"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"os"
	"strings"
	"time"
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

type ExecutionSurface struct {
	WorkspaceRoot    string
	IsolationRoot    string
	IsolationEnabled bool

	sandbox           sandbox.Sandbox
	Workspace         workspace.Workspace
	Executor          workspace.Executor
	Isolation         workspace.WorkspaceIsolation
	RepoStateCapture  workspace.RepoStateCapture
	PatchApply        workspace.PatchApply
	PatchRevert       workspace.PatchRevert
	WorktreeSnapshots workspace.WorktreeSnapshotStore

	errors   map[string]error
	disabled map[string]bool
}

func newExecutionSurface(sb sandbox.Sandbox, ws workspace.Workspace, exec workspace.Executor) *ExecutionSurface {
	return &ExecutionSurface{
		sandbox:   sb,
		Workspace: ws,
		Executor:  exec,
		errors:    map[string]error{},
		disabled:  map[string]bool{},
	}
}

func NewExecutionSurface(workspace, isolationRoot string, enableIsolation bool) *ExecutionSurface {
	surface := &ExecutionSurface{
		WorkspaceRoot:    strings.TrimSpace(workspace),
		IsolationRoot:    strings.TrimSpace(isolationRoot),
		IsolationEnabled: enableIsolation,
		errors:           map[string]error{},
		disabled:         map[string]bool{},
	}
	if enableIsolation {
		if surface.IsolationRoot == "" {
			surface.errors[CapabilityExecutionIsolation] = errors.New("workspace isolation root is empty")
		} else if isolation, err := sandbox.NewLocalWorkspaceIsolation(surface.IsolationRoot); err != nil {
			surface.errors[CapabilityExecutionIsolation] = err
		} else {
			surface.Isolation = isolation
		}
	} else {
		surface.disabled[CapabilityExecutionIsolation] = true
	}
	surface.RepoStateCapture = sandbox.NewGitRepoStateCapture(surface.WorkspaceRoot)
	surface.PatchApply = sandbox.NewGitPatchApply(surface.WorkspaceRoot)
	surface.PatchRevert = sandbox.NewGitPatchRevert(surface.WorkspaceRoot)
	surface.WorktreeSnapshots = sandbox.NewGitWorktreeSnapshotStore(surface.WorkspaceRoot)
	return surface
}

func ProbeExecutionSurface(workspace, isolationRoot string, enableIsolation bool) *ExecutionSurface {
	surface := NewExecutionSurface(workspace, isolationRoot, enableIsolation)
	sb, err := sandbox.NewLocal(strings.TrimSpace(workspace))
	if err != nil {
		surface.errors[CapabilityExecutionWorkspace] = err
		surface.errors[CapabilityExecutionExecutor] = err
		return surface
	}
	surface.Workspace = sandbox.NewLocalWorkspace(sb)
	surface.Executor = sandbox.NewLocalExecutor(sb)
	return surface
}

func ExecutionSurfaceFromKernel(k *kernel.Kernel, workspace, isolationRoot string, enableIsolation bool) *ExecutionSurface {
	if k == nil {
		return ProbeExecutionSurface(workspace, isolationRoot, enableIsolation)
	}
	surface := &ExecutionSurface{
		WorkspaceRoot:     strings.TrimSpace(workspace),
		IsolationRoot:     strings.TrimSpace(isolationRoot),
		IsolationEnabled:  enableIsolation,
		Workspace:         k.Workspace(),
		Executor:          k.Executor(),
		Isolation:         k.WorkspaceIsolation(),
		RepoStateCapture:  k.RepoStateCapture(),
		PatchApply:        k.PatchApply(),
		PatchRevert:       k.PatchRevert(),
		WorktreeSnapshots: k.WorktreeSnapshots(),
		errors:            map[string]error{},
		disabled:          map[string]bool{},
	}
	if !enableIsolation {
		surface.disabled[CapabilityExecutionIsolation] = true
	}
	return surface
}

func (s *ExecutionSurface) KernelOptions() []kernel.Option {
	if s == nil {
		return nil
	}
	opts := make([]kernel.Option, 0, 5)
	if s.Isolation != nil {
		opts = append(opts, kernel.WithWorkspaceIsolation(s.Isolation))
	}
	if s.RepoStateCapture != nil {
		opts = append(opts, kernel.WithRepoStateCapture(s.RepoStateCapture))
	}
	if s.PatchApply != nil {
		opts = append(opts, kernel.WithPatchApply(s.PatchApply))
	}
	if s.PatchRevert != nil {
		opts = append(opts, kernel.WithPatchRevert(s.PatchRevert))
	}
	if s.WorktreeSnapshots != nil {
		opts = append(opts, kernel.WithWorktreeSnapshots(s.WorktreeSnapshots))
	}
	return opts
}

func (s *ExecutionSurface) Sandbox() sandbox.Sandbox {
	if s == nil {
		return nil
	}
	return s.sandbox
}

func (s *ExecutionSurface) WorkspacePort() workspace.Workspace {
	if s == nil {
		return nil
	}
	if s.Workspace != nil {
		return s.Workspace
	}
	if s.sandbox == nil {
		return nil
	}
	return &kernelWorkspaceAdapter{sb: s.sandbox}
}

func (s *ExecutionSurface) HasWorkspace() bool {
	return s != nil && (s.Workspace != nil || s.sandbox != nil)
}

func (s *ExecutionSurface) HasExecutor() bool {
	return s != nil && (s.Executor != nil || s.sandbox != nil)
}

func (s *ExecutionSurface) ExecutorPort() workspace.Executor {
	if s == nil {
		return nil
	}
	if s.Executor != nil {
		return s.Executor
	}
	if s.sandbox == nil {
		return nil
	}
	return &kernelExecutorAdapter{sb: s.sandbox}
}

func (s *ExecutionSurface) Error(capability string) error {
	if s == nil {
		return nil
	}
	return s.errors[strings.TrimSpace(capability)]
}

func (s *ExecutionSurface) CapabilityStatuses() []CapabilityStatus {
	if s == nil {
		return nil
	}
	return []CapabilityStatus{
		s.capabilityStatus(CapabilityExecutionWorkspace, "workspace", s.Workspace != nil, true),
		s.capabilityStatus(CapabilityExecutionExecutor, "executor", s.Executor != nil, true),
		s.capabilityStatus(CapabilityExecutionIsolation, "workspace_isolation", s.Isolation != nil, false),
		s.capabilityStatus(CapabilityExecutionRepoState, "repo_state_capture", s.RepoStateCapture != nil, false),
		s.capabilityStatus(CapabilityExecutionPatchApply, "patch_apply", s.PatchApply != nil, false),
		s.capabilityStatus(CapabilityExecutionPatchRevert, "patch_revert", s.PatchRevert != nil, false),
		s.capabilityStatus(CapabilityExecutionWorktreeStates, "worktree_snapshots", s.WorktreeSnapshots != nil, false),
	}
}

func ReportExecutionSurface(ctx context.Context, reporter CapabilityReporter, surface *ExecutionSurface) {
	if reporter == nil || surface == nil {
		return
	}
	for _, status := range surface.CapabilityStatuses() {
		reporter.Report(ctx, status.Capability, status.Critical, status.State, surface.Error(status.Capability))
	}
}

func (s *ExecutionSurface) capabilityStatus(capability, name string, ready bool, critical bool) CapabilityStatus {
	status := CapabilityStatus{
		Capability: capability,
		Kind:       "execution",
		Name:       name,
		Critical:   critical,
	}
	if s.disabled[capability] {
		status.State = "disabled"
		return status
	}
	if ready {
		status.State = "ready"
		return status
	}
	if err := s.errors[capability]; err != nil {
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

type kernelWorkspaceAdapter struct {
	sb sandbox.Sandbox
}

type kernelExecutorAdapter struct {
	sb sandbox.Sandbox
}

func (a *kernelExecutorAdapter) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	out, err := a.sb.Execute(ctx, req)
	if err != nil {
		return workspace.ExecOutput{}, err
	}
	return workspace.ExecOutput{
		Stdout:      out.Stdout,
		Stderr:      out.Stderr,
		ExitCode:    out.ExitCode,
		Enforcement: out.Enforcement,
		Degraded:    out.Degraded,
		Details:     out.Details,
	}, nil
}

func (a *kernelWorkspaceAdapter) ReadFile(_ context.Context, path string) ([]byte, error) {
	return a.sb.ReadFile(path)
}

func (a *kernelWorkspaceAdapter) WriteFile(_ context.Context, path string, content []byte) error {
	return a.sb.WriteFile(path, content)
}

func (a *kernelWorkspaceAdapter) ListFiles(_ context.Context, pattern string) ([]string, error) {
	return a.sb.ListFiles(pattern)
}

func (a *kernelWorkspaceAdapter) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, err := a.sb.ResolvePath(path)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	return workspace.FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime().UTC().Round(time.Second),
	}, nil
}

func (a *kernelWorkspaceAdapter) DeleteFile(_ context.Context, path string) error {
	resolved, err := a.sb.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}
