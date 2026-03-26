package agentsx

import (
	"context"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/kernel"
	kerrors "github.com/mossagents/moss/kernel/errors"
)

const stateKey kernel.ExtensionStateKey = "agentsx.state"

type state struct {
	registry *agent.Registry
	tasks    *agent.TaskTracker
}

// WithRegistry 设置 Agent 注册表。
func WithRegistry(r *agent.Registry) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).registry = r
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
		if st.tasks == nil {
			st.tasks = agent.NewTaskTracker()
		}
		if err := agent.RegisterTools(k.ToolRegistry(), st.registry, st.tasks, k); err != nil {
			return kerrors.Wrap(kerrors.ErrInternal, "register agent delegation tools", err)
		}
		return nil
	})
	return st
}
