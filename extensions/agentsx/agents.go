package agentsx

import (
	"context"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/kernel"
	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/port"
)

const stateKey kernel.ExtensionStateKey = "agentsx.state"

type state struct {
	registry  *agent.Registry
	tasks     *agent.TaskTracker
	runtime   port.TaskRuntime
	mailbox   port.Mailbox
	isolation port.WorkspaceIsolation
}

// WithRegistry 设置 Agent 注册表。
func WithRegistry(r *agent.Registry) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).registry = r
	}
}

// WithTaskRuntime 设置协作任务运行时。
func WithTaskRuntime(rt port.TaskRuntime) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).runtime = rt
	}
}

// WithMailbox 设置协作邮箱。
func WithMailbox(mb port.Mailbox) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).mailbox = mb
	}
}

// WithWorkspaceIsolation 设置隔离工作区提供者。
func WithWorkspaceIsolation(isolation port.WorkspaceIsolation) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).isolation = isolation
	}
}

// Registry 返回当前 Kernel 绑定的 Agent 注册表。
func Registry(k *kernel.Kernel) *agent.Registry {
	return ensureState(k).registry
}

// TaskTracker 返回当前 Agent 工具链的异步任务跟踪器。
func TaskTracker(k *kernel.Kernel) *agent.TaskTracker {
	return ensureState(k).tasks
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{
		registry: agent.NewRegistry(),
	})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnBoot(100, func(_ context.Context, k *kernel.Kernel) error {
		if st.registry == nil || len(st.registry.List()) == 0 {
			return nil
		}
		if st.runtime == nil {
			st.runtime = k.TaskRuntime()
		}
		if st.runtime == nil {
			st.runtime = port.NewMemoryTaskRuntime()
		}
		if st.mailbox == nil {
			st.mailbox = k.Mailbox()
		}
		if st.mailbox == nil {
			st.mailbox = port.NewMemoryMailbox()
		}
		if st.isolation == nil {
			st.isolation = k.WorkspaceIsolation()
		}
		if st.tasks == nil {
			st.tasks = agent.NewTaskTrackerWithRuntime(st.runtime)
		}
		if err := agent.RegisterToolsWithDeps(k.ToolRegistry(), st.registry, st.tasks, k, agent.RuntimeDeps{
			TaskRuntime: st.runtime,
			Mailbox:     st.mailbox,
			Isolation:   st.isolation,
		}); err != nil {
			return kerrors.Wrap(kerrors.ErrInternal, "register agent delegation tools", err)
		}
		return nil
	})
	return st
}
