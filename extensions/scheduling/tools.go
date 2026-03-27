package scheduling

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
)

// RegisterScheduleTools 注册调度相关工具。
// Deprecated: use runtime.RegisterSchedulerToolRegistry.
func RegisterScheduleTools(reg tool.Registry, sched *scheduler.Scheduler) error {
	return runtime.RegisterSchedulerToolRegistry(reg, sched)
}
