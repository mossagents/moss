package scheduling

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/scheduler"
	toolbuiltins "github.com/mossagents/moss/kernel/tool/builtins"
)

// WithScheduler 将调度器作为标准扩展接入 Kernel。
func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return func(k *kernel.Kernel) {
		kernel.Extensions(k).SetScheduler(s)
	}
}

// RegisterTools 为调度器注册标准工具集。
func RegisterTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return toolbuiltins.RegisterScheduleTools(k.ToolRegistry(), sched)
}
