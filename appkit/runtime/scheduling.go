package runtime

import (
	"github.com/mossagents/moss/extensions/scheduling"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
)

func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return scheduling.WithScheduler(s)
}

func RegisterSchedulerTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return scheduling.RegisterTools(k, sched)
}

func RegisterSchedulerToolRegistry(reg tool.Registry, sched *scheduler.Scheduler) error {
	return scheduling.RegisterToolRegistry(reg, sched)
}
