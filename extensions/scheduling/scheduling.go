package scheduling

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
)

// WithScheduler 将调度器作为标准扩展接入 Kernel。
// Deprecated: use runtime.WithScheduler.
func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return runtime.WithScheduler(s)
}

// RegisterTools 为调度器注册标准工具集。
// Deprecated: use runtime.RegisterSchedulerTools.
func RegisterTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return runtime.RegisterSchedulerTools(k, sched)
}

// RegisterToolRegistry 为调度器注册标准工具集。
// Deprecated: use runtime.RegisterSchedulerToolRegistry.
func RegisterToolRegistry(reg tool.Registry, sched *scheduler.Scheduler) error {
	return runtime.RegisterSchedulerToolRegistry(reg, sched)
}
