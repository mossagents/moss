package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/mossagents/moss/kernel"
	taskrt "github.com/mossagents/moss/kernel/task"
)

const (
	kernelServiceKey    kernel.ServiceKey = "agent.kernel_service"
	delegationBootOrder                   = 100
)

type kernelService struct {
	mu sync.Mutex

	registry *Registry
	tasks    *TaskTracker

	bootHookRegistered bool
	installed          bool
}

// ensureKernelService owns the agent substrate slot on the kernel service registry.
func ensureKernelService(k *kernel.Kernel) *kernelService {
	actual, _ := k.Services().LoadOrStore(kernelServiceKey, &kernelService{
		registry: NewRegistry(),
	})
	st := actual.(*kernelService)
	st.mu.Lock()
	if st.registry == nil {
		st.registry = NewRegistry()
	}
	st.mu.Unlock()
	return st
}

// KernelRegistry returns the kernel-scoped agent registry owned by the agent substrate.
func KernelRegistry(k *kernel.Kernel) *Registry {
	if k == nil {
		return nil
	}
	st := ensureKernelService(k)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.registry == nil {
		st.registry = NewRegistry()
	}
	return st.registry
}

// SetKernelRegistry replaces the kernel-scoped registry before delegation tools are installed.
func SetKernelRegistry(k *kernel.Kernel, reg *Registry) error {
	if k == nil {
		return fmt.Errorf("kernel must not be nil")
	}
	if reg == nil {
		return fmt.Errorf("agent registry must not be nil")
	}
	st := ensureKernelService(k)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.installed {
		return fmt.Errorf("agent registry cannot be replaced after delegation tools are installed")
	}
	st.registry = reg
	return nil
}

// EnsureKernelDelegation guarantees that delegation tools will be installed for this kernel.
// Before boot starts it queues a boot hook; once boot has started it installs synchronously.
func EnsureKernelDelegation(k *kernel.Kernel) error {
	if k == nil {
		return fmt.Errorf("kernel must not be nil")
	}
	st := ensureKernelService(k)
	st.mu.Lock()
	if st.installed {
		st.mu.Unlock()
		return nil
	}
	if !k.Stages().BootStarted() {
		if !st.bootHookRegistered {
			st.bootHookRegistered = true
			k.Stages().OnBoot(delegationBootOrder, func(_ context.Context, k *kernel.Kernel) error {
				return installKernelDelegation(k)
			})
		}
		st.mu.Unlock()
		return nil
	}
	st.mu.Unlock()
	return installKernelDelegation(k)
}

func installKernelDelegation(k *kernel.Kernel) error {
	st := ensureKernelService(k)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.installed {
		return nil
	}
	reg := st.registry
	if reg == nil {
		reg = NewRegistry()
		st.registry = reg
	}
	rt := k.TaskRuntime()
	if rt == nil {
		rt = taskrt.NewMemoryTaskRuntime()
	}
	mb := k.Mailbox()
	if mb == nil {
		mb = taskrt.NewMemoryMailbox()
	}
	if st.tasks == nil {
		st.tasks = NewTaskTrackerWithRuntime(rt)
	}
	if err := RegisterToolsWithDeps(k.ToolRegistry(), reg, st.tasks, k, RuntimeDeps{
		TaskRuntime: rt,
		Mailbox:     mb,
		Isolation:   k.WorkspaceIsolation(),
	}); err != nil {
		return fmt.Errorf("register agent delegation tools: %w", err)
	}
	st.installed = true
	return nil
}
