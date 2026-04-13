package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	kt "github.com/mossagents/moss/testing"
)

// --- test helpers ---

type stubWorkspace struct{ workspace.Workspace }

func (stubWorkspace) ReadFile(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (stubWorkspace) WriteFile(_ context.Context, _ string, _ []byte) error   { return nil }
func (stubWorkspace) ListFiles(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (stubWorkspace) Stat(_ context.Context, _ string) (workspace.FileInfo, error) {
	return workspace.FileInfo{}, nil
}
func (stubWorkspace) DeleteFile(_ context.Context, _ string) error { return nil }

type stubExecutor struct{ workspace.Executor }

func (stubExecutor) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{}, nil
}

type stubManagedBackend struct {
	workspace.Workspace
	workspace.Executor
	installed int
	booted    int
	shutdowns int
}

func newStubManagedBackend() *stubManagedBackend {
	return &stubManagedBackend{
		Workspace: stubWorkspace{},
		Executor:  stubExecutor{},
	}
}

func (b *stubManagedBackend) Install(_ context.Context, k *kernel.Kernel) error {
	b.installed++
	k.Apply(
		kernel.WithWorkspace(b.Workspace),
		kernel.WithExecutor(b.Executor),
	)
	return nil
}

func (b *stubManagedBackend) Boot(_ context.Context, _ *kernel.Kernel) error {
	b.booted++
	return nil
}

func (b *stubManagedBackend) Shutdown(_ context.Context, _ *kernel.Kernel) error {
	b.shutdowns++
	return nil
}

func newTestHarness() *Harness {
	k := kernel.New()
	backend := &LocalBackend{
		Workspace: stubWorkspace{},
		Executor:  stubExecutor{},
	}
	return New(k, backend)
}

func newSandboxHarness(t *testing.T, root string) *Harness {
	t.Helper()
	sb, err := sandbox.NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return New(kernel.New(), &LocalBackend{Sandbox: sb})
}

type recordingCapabilityReporter struct {
	calls []string
}

func (r *recordingCapabilityReporter) Report(_ context.Context, capability string, _ bool, state string, _ error) {
	r.calls = append(r.calls, capability+":"+state)
}

// --- tests ---

func TestNew(t *testing.T) {
	h := newTestHarness()
	if h.Kernel() == nil {
		t.Fatal("Kernel() should not be nil")
	}
	if h.Backend() == nil {
		t.Fatal("Backend() should not be nil")
	}
	if len(h.InstalledFeatures()) != 0 {
		t.Fatal("InstalledFeatures() should be empty initially")
	}
}

func TestInstall_SingleFeature(t *testing.T) {
	h := newTestHarness()
	called := false
	f := FeatureFunc{
		FeatureName: "test-feature",
		InstallFunc: func(_ context.Context, _ *Harness) error {
			called = true
			return nil
		},
	}
	if err := h.Install(context.Background(), f); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if !called {
		t.Fatal("Install should have called the feature's InstallFunc")
	}
	if len(h.InstalledFeatures()) != 1 {
		t.Fatalf("expected 1 installed feature, got %d", len(h.InstalledFeatures()))
	}
	if h.InstalledFeatures()[0].Name() != "test-feature" {
		t.Fatalf("expected feature name %q, got %q", "test-feature", h.InstalledFeatures()[0].Name())
	}
}

func TestInstall_MultipleFeatures_InOrder(t *testing.T) {
	h := newTestHarness()
	var order []string
	mkFeature := func(name string) Feature {
		return FeatureFunc{
			FeatureName: name,
			InstallFunc: func(_ context.Context, _ *Harness) error {
				order = append(order, name)
				return nil
			},
		}
	}
	err := h.Install(context.Background(), mkFeature("a"), mkFeature("b"), mkFeature("c"))
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("expected [a b c], got %v", order)
	}
}

func TestInstall_GovernsByPhaseAndDependency(t *testing.T) {
	h := newTestHarness()
	var order []string
	mk := func(name string, meta FeatureMetadata) Feature {
		return FeatureFunc{
			FeatureName:   name,
			MetadataValue: meta,
			InstallFunc: func(_ context.Context, _ *Harness) error {
				order = append(order, name)
				return nil
			},
		}
	}
	err := h.Install(context.Background(),
		mk("late", FeatureMetadata{Phase: FeaturePhasePostRuntime}),
		mk("context", FeatureMetadata{Key: "context", Requires: []string{"session-store"}}),
		mk("runtime", FeatureMetadata{Key: "runtime-setup", Phase: FeaturePhaseRuntime}),
		mk("session-store", FeatureMetadata{Key: "session-store"}),
	)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	want := []string{"session-store", "context", "runtime", "late"}
	if len(order) != len(want) {
		t.Fatalf("expected %v, got %v", want, order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, order)
		}
	}
	got := h.InstalledFeatures()
	for i := range want {
		if got[i].Name() != want[i] {
			t.Fatalf("installed features order = %v, want %v", []string{got[0].Name(), got[1].Name(), got[2].Name(), got[3].Name()}, want)
		}
	}
}

func TestInstall_MissingFeatureDependencyFailsBeforeInstall(t *testing.T) {
	h := newTestHarness()
	called := false
	err := h.Install(context.Background(), FeatureFunc{
		FeatureName:   "context",
		MetadataValue: FeatureMetadata{Key: "context", Requires: []string{"session-store"}},
		InstallFunc: func(_ context.Context, _ *Harness) error {
			called = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "session-store") {
		t.Fatalf("expected missing dependency in error, got %v", err)
	}
	if called {
		t.Fatal("feature should not be installed when planning fails")
	}
	if len(h.InstalledFeatures()) != 0 {
		t.Fatal("expected no installed features after planning failure")
	}
}

func TestInstall_NilFeatureSkipped(t *testing.T) {
	h := newTestHarness()
	called := false
	f := FeatureFunc{
		FeatureName: "real",
		InstallFunc: func(_ context.Context, _ *Harness) error {
			called = true
			return nil
		},
	}
	if err := h.Install(context.Background(), nil, f); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("non-nil feature should still be installed")
	}
	if len(h.InstalledFeatures()) != 1 {
		t.Fatalf("expected 1 installed feature, got %d", len(h.InstalledFeatures()))
	}
}

func TestInstall_ErrorStopsChain(t *testing.T) {
	h := newTestHarness()
	boom := errors.New("boom")
	secondCalled := false
	err := h.Install(context.Background(),
		FeatureFunc{FeatureName: "bad", InstallFunc: func(_ context.Context, _ *Harness) error { return boom }},
		FeatureFunc{FeatureName: "good", InstallFunc: func(_ context.Context, _ *Harness) error {
			secondCalled = true
			return nil
		}},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got: %v", err)
	}
	if secondCalled {
		t.Fatal("second feature should not have been called after error")
	}
	if len(h.InstalledFeatures()) != 0 {
		t.Fatal("no features should be recorded after error")
	}
}

func TestLocalBackend_ImplementsBackend(t *testing.T) {
	var _ Backend = &LocalBackend{}
}

func TestNewWithBackendFactory_ActivatesLifecycle(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
	)
	backend := newStubManagedBackend()
	builds := 0

	h, err := NewWithBackendFactory(context.Background(), k, BackendFactoryFunc(func(context.Context, *kernel.Kernel) (Backend, error) {
		builds++
		return backend, nil
	}))
	if err != nil {
		t.Fatalf("NewWithBackendFactory: %v", err)
	}
	if h.Backend() != backend {
		t.Fatal("expected factory backend to be attached to harness")
	}
	if builds != 1 {
		t.Fatalf("build count = %d, want 1", builds)
	}
	if backend.installed != 1 {
		t.Fatalf("install count = %d, want 1", backend.installed)
	}
	if k.Workspace() == nil || k.Executor() == nil {
		t.Fatal("expected managed backend to wire kernel workspace and executor")
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if backend.booted != 1 {
		t.Fatalf("boot count = %d, want 1", backend.booted)
	}
	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if backend.shutdowns != 1 {
		t.Fatalf("shutdown count = %d, want 1", backend.shutdowns)
	}
}

func TestInstall_ActivatesManagedBackendBeforeFeatureInstall(t *testing.T) {
	k := kernel.New()
	backend := newStubManagedBackend()
	h := New(k, backend)
	sawInstalledBackend := false

	err := h.Install(context.Background(), FeatureFunc{
		FeatureName: "check-backend",
		InstallFunc: func(_ context.Context, h *Harness) error {
			sawInstalledBackend = backend.installed == 1 &&
				h.Kernel().Workspace() != nil &&
				h.Kernel().Executor() != nil &&
				h.Backend() == backend
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !sawInstalledBackend {
		t.Fatal("expected managed backend to activate before feature install")
	}
}

func TestLocalBackendFactory_InstallsPortsWhenMissing(t *testing.T) {
	k := kernel.New()
	h, err := NewWithBackendFactory(context.Background(), k, NewLocalBackendFactory(t.TempDir()))
	if err != nil {
		t.Fatalf("NewWithBackendFactory: %v", err)
	}
	backend, ok := h.Backend().(*LocalBackend)
	if !ok {
		t.Fatalf("backend type = %T, want *LocalBackend", h.Backend())
	}
	if k.Sandbox() == nil {
		t.Fatal("expected local backend factory to install sandbox")
	}
	if k.Workspace() == nil || k.Executor() == nil {
		t.Fatal("expected local backend factory to install workspace and executor")
	}
	if backend.Sandbox == nil || backend.Workspace == nil || backend.Executor == nil {
		t.Fatal("expected activated local backend to adopt effective kernel ports")
	}
}

func TestLocalBackendFactory_PreservesExistingKernelPorts(t *testing.T) {
	ws := stubWorkspace{}
	exec := stubExecutor{}
	k := kernel.New(
		kernel.WithWorkspace(ws),
		kernel.WithExecutor(exec),
	)
	h, err := NewWithBackendFactory(context.Background(), k, NewLocalBackendFactory(t.TempDir()))
	if err != nil {
		t.Fatalf("NewWithBackendFactory: %v", err)
	}
	backend, ok := h.Backend().(*LocalBackend)
	if !ok {
		t.Fatalf("backend type = %T, want *LocalBackend", h.Backend())
	}
	if got := k.Workspace(); got != ws {
		t.Fatalf("workspace = %#v, want %#v", got, ws)
	}
	if got := k.Executor(); got != exec {
		t.Fatalf("executor = %#v, want %#v", got, exec)
	}
	if backend.Workspace != ws || backend.Executor != exec {
		t.Fatal("expected local backend to adopt existing kernel ports")
	}
}

func TestFeature_BootstrapContext(t *testing.T) {
	f := BootstrapContext("/nonexistent", "test-app", "trusted")
	if f.Name() != "bootstrap-context" {
		t.Fatalf("expected name %q, got %q", "bootstrap-context", f.Name())
	}
	h := newTestHarness()
	// Install should not fail even with a non-existent workspace (bootstrap
	// gracefully handles missing files).
	if err := h.Install(context.Background(), f); err != nil {
		t.Fatalf("BootstrapContext Install failed: %v", err)
	}
}

func TestFeature_SessionPersistence_NilStore(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), SessionPersistence(nil))
	if err == nil {
		t.Fatal("expected error for nil session store")
	}
}

func TestFeature_Checkpointing_NilStore(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), Checkpointing(nil))
	if err == nil {
		t.Fatal("expected error for nil checkpoint store")
	}
}

func TestFeature_TaskDelegation_NilRuntime(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), TaskDelegation(nil))
	if err == nil {
		t.Fatal("expected error for nil task runtime")
	}
}

func TestFeature_StateCatalog_NilCatalog(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), StateCatalog(nil))
	if err == nil {
		t.Fatal("expected error for nil state catalog")
	}
}

func TestFeature_ExecutionServices_EmptyWorkspaceRoot(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), ExecutionServices("", "", false))
	if err == nil {
		t.Fatal("expected error for empty execution workspace root")
	}
}

func TestFeature_ExecutionServices_RequiresPorts(t *testing.T) {
	h := New(kernel.New(), nil)
	err := h.Install(context.Background(), ExecutionServices(t.TempDir(), "", false))
	if err == nil {
		t.Fatal("expected error when backend ports are missing")
	}
	if !strings.Contains(err.Error(), "workspace port") {
		t.Fatalf("expected workspace-port error, got %v", err)
	}
}

func TestFeature_ExecutionServices_WorkspaceRootMismatch(t *testing.T) {
	h := newSandboxHarness(t, t.TempDir())
	err := h.Install(context.Background(), ExecutionServices(t.TempDir(), "", false))
	if err == nil {
		t.Fatal("expected error for workspace root mismatch")
	}
	if !strings.Contains(err.Error(), "does not match backend root") {
		t.Fatalf("expected root mismatch error, got %v", err)
	}
}

func TestFeature_ExecutionServices_InstallsAuxiliaryPorts(t *testing.T) {
	workspaceDir := t.TempDir()
	isolationRoot := t.TempDir()
	h := newSandboxHarness(t, workspaceDir)
	if err := h.Install(context.Background(), ExecutionServices(workspaceDir, isolationRoot, true)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if h.Kernel().WorkspaceIsolation() == nil {
		t.Fatal("expected workspace isolation to be installed")
	}
	if h.Kernel().RepoStateCapture() == nil {
		t.Fatal("expected repo state capture to be installed")
	}
	if h.Kernel().PatchApply() == nil {
		t.Fatal("expected patch apply to be installed")
	}
	if h.Kernel().PatchRevert() == nil {
		t.Fatal("expected patch revert to be installed")
	}
	if h.Kernel().WorktreeSnapshots() == nil {
		t.Fatal("expected worktree snapshots to be installed")
	}
}

func TestFeature_ExecutionCapabilityReport_CustomReporter(t *testing.T) {
	workspaceDir := t.TempDir()
	h := newSandboxHarness(t, workspaceDir)
	reporter := &recordingCapabilityReporter{}
	if err := h.Install(context.Background(),
		ExecutionServices(workspaceDir, "", false),
		ExecutionCapabilityReport(workspaceDir, "", false, reporter),
	); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(reporter.calls) == 0 {
		t.Fatal("expected execution capability report calls")
	}
}

func TestFeature_LLMResilience(t *testing.T) {
	h := newTestHarness()
	cfg := &retry.Config{MaxRetries: 3}
	err := h.Install(context.Background(), LLMResilience(cfg, nil))
	if err != nil {
		t.Fatalf("LLMResilience Install failed: %v", err)
	}
	if len(h.InstalledFeatures()) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(h.InstalledFeatures()))
	}
}

func TestFeature_LLMResilience_BothNil(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), LLMResilience(nil, nil))
	if err != nil {
		t.Fatalf("LLMResilience with both nil should succeed: %v", err)
	}
}

func TestFeature_PatchToolCalls(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), PatchToolCalls())
	if err != nil {
		t.Fatalf("PatchToolCalls Install failed: %v", err)
	}
}

func TestFeature_ExecutionPolicy(t *testing.T) {
	h := newTestHarness()
	err := h.Install(context.Background(), ExecutionPolicy())
	if err != nil {
		t.Fatalf("ExecutionPolicy Install failed: %v", err)
	}
}

func TestKernel_Apply(t *testing.T) {
	k := kernel.New()
	if k.Checkpoints() != nil {
		t.Fatal("expected nil checkpoint store initially")
	}
	// Apply is used by harness features to set kernel options post-construction.
	// We can verify it works by checking any observable side-effect.
	// Since Checkpoints is nil by default, we can't easily verify
	// the Apply call without a real store, so just ensure it doesn't panic.
	k.Apply()
}

func TestHarness_FeatureAccessesKernelAndBackend(t *testing.T) {
	h := newTestHarness()
	var gotKernel *kernel.Kernel
	var gotBackend Backend
	f := FeatureFunc{
		FeatureName: "introspect",
		InstallFunc: func(_ context.Context, h *Harness) error {
			gotKernel = h.Kernel()
			gotBackend = h.Backend()
			return nil
		},
	}
	if err := h.Install(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if gotKernel != h.Kernel() {
		t.Fatal("feature should see the same kernel")
	}
	if gotBackend != h.Backend() {
		t.Fatal("feature should see the same backend")
	}
}
