package agent

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/workspace"
	kt "github.com/mossagents/moss/testing"
)

func TestKernelRegistryReturnsSingletonRegistry(t *testing.T) {
	k := newTestKernel()
	first := KernelRegistry(k)
	second := KernelRegistry(k)
	if first == nil || second == nil {
		t.Fatal("expected kernel registry")
	}
	if first != second {
		t.Fatal("expected KernelRegistry to return the same registry instance for one kernel")
	}
}

func TestEnsureKernelDelegationQueuesBootInstallationBeforeBoot(t *testing.T) {
	k := newTestKernel()
	if err := EnsureKernelDelegation(k); err != nil {
		t.Fatalf("EnsureKernelDelegation: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("delegate_agent"); ok {
		t.Fatal("delegate_agent should not be installed before boot")
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("delegate_agent"); !ok {
		t.Fatal("expected delegate_agent after boot installation")
	}
}

func TestEnsureKernelDelegationInstallsSynchronouslyDuringBoot(t *testing.T) {
	k := newTestKernel()
	called := false
	k.Stages().OnBoot(10, func(context.Context, *kernel.Kernel) error {
		called = true
		if err := EnsureKernelDelegation(k); err != nil {
			return err
		}
		if _, ok := k.ToolRegistry().Get("delegate_agent"); !ok {
			t.Fatal("expected delegate_agent to be available in the same boot pass")
		}
		return nil
	})
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if !called {
		t.Fatal("expected boot hook to run")
	}
}

func TestSetKernelRegistryAllowsPreBootReplacementAndRejectsPostInstall(t *testing.T) {
	k := newTestKernel()
	first := NewRegistry()
	second := NewRegistry()
	if err := SetKernelRegistry(k, first); err != nil {
		t.Fatalf("SetKernelRegistry first: %v", err)
	}
	if err := SetKernelRegistry(k, second); err != nil {
		t.Fatalf("SetKernelRegistry second: %v", err)
	}
	if got := KernelRegistry(k); got != second {
		t.Fatal("expected latest pre-boot registry replacement to win")
	}
	if err := EnsureKernelDelegation(k); err != nil {
		t.Fatalf("EnsureKernelDelegation: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if err := SetKernelRegistry(k, NewRegistry()); err == nil {
		t.Fatal("expected post-install registry replacement to fail")
	}
}

func TestEnsureKernelDelegationUsesKernelPortsAndFallbacks(t *testing.T) {
	rt := taskrt.NewMemoryTaskRuntime()
	mb := taskrt.NewMemoryMailbox()
	iso := stubIsolation{}
	k := newTestKernel(
		kernel.WithTaskRuntime(rt),
		kernel.WithMailbox(mb),
		kernel.WithWorkspaceIsolation(iso),
	)
	if err := EnsureKernelDelegation(k); err != nil {
		t.Fatalf("EnsureKernelDelegation: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	st := ensureKernelService(k)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.tasks == nil {
		t.Fatal("expected task tracker")
	}
	if st.tasks.runtime != rt {
		t.Fatal("expected task tracker to use the kernel task runtime")
	}
	if _, ok := k.ToolRegistry().Get("send_mail"); !ok {
		t.Fatal("expected mailbox-backed tool registration")
	}
	if _, ok := k.ToolRegistry().Get("acquire_workspace"); !ok {
		t.Fatal("expected isolation-backed tool registration")
	}

	fallbackKernel := newTestKernel()
	if err := EnsureKernelDelegation(fallbackKernel); err != nil {
		t.Fatalf("EnsureKernelDelegation fallback: %v", err)
	}
	if err := fallbackKernel.Boot(context.Background()); err != nil {
		t.Fatalf("Boot fallback: %v", err)
	}
	fallbackState := ensureKernelService(fallbackKernel)
	fallbackState.mu.Lock()
	defer fallbackState.mu.Unlock()
	if fallbackState.tasks == nil || fallbackState.tasks.runtime == nil {
		t.Fatal("expected fallback task runtime to be installed")
	}
	if _, ok := fallbackKernel.ToolRegistry().Get("send_mail"); !ok {
		t.Fatal("expected fallback mailbox-backed tool registration")
	}
}

func newTestKernel(opts ...kernel.Option) *kernel.Kernel {
	base := []kernel.Option{
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(kt.NewRecorderIO()),
	}
	base = append(base, opts...)
	return kernel.New(base...)
}

type stubIsolation struct{}

func (stubIsolation) Acquire(context.Context, string) (*workspace.WorkspaceLease, error) {
	return &workspace.WorkspaceLease{WorkspaceID: "iso-1"}, nil
}

func (stubIsolation) Release(context.Context, string) error { return nil }
