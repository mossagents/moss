package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/workspace"
)

// --- test helpers ---

type stubWorkspace struct{ workspace.Workspace }

func (stubWorkspace) ReadFile(_ context.Context, _ string) ([]byte, error)     { return nil, nil }
func (stubWorkspace) WriteFile(_ context.Context, _ string, _ []byte) error    { return nil }
func (stubWorkspace) ListFiles(_ context.Context, _ string) ([]string, error)  { return nil, nil }
func (stubWorkspace) Stat(_ context.Context, _ string) (workspace.FileInfo, error) {
	return workspace.FileInfo{}, nil
}
func (stubWorkspace) DeleteFile(_ context.Context, _ string) error { return nil }

type stubExecutor struct{ workspace.Executor }

func (stubExecutor) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{}, nil
}

func newTestHarness() *Harness {
	k := kernel.New()
	backend := &LocalBackend{
		Workspace: stubWorkspace{},
		Executor:  stubExecutor{},
	}
	return New(k, backend)
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
