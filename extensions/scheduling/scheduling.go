package scheduling

import (
	"context"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
)

const stateKey kernel.ExtensionStateKey = "scheduling.state"

type state struct {
	scheduler *scheduler.Scheduler
}

// WithScheduler 将调度器作为标准扩展接入 Kernel。
func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).scheduler = s
	}
}

// RegisterTools 为调度器注册标准工具集。
func RegisterTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return RegisterScheduleTools(k.ToolRegistry(), sched)
}

// RegisterToolRegistry 为调度器注册标准工具集。
func RegisterToolRegistry(reg tool.Registry, sched *scheduler.Scheduler) error {
	return RegisterScheduleTools(reg, sched)
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnShutdown(200, func(_ context.Context, _ *kernel.Kernel) error {
		if st.scheduler != nil {
			st.scheduler.Stop()
		}
		return nil
	})
	return st
}
