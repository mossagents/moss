package runtime

import (
	"context"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/runtime/scheduling"
	"github.com/mossagents/moss/scheduler"
)

// Scheduling type aliases for backward compatibility.
type ScheduleItem = scheduling.ScheduleItem
type ScheduleController = scheduling.ScheduleController
type SchedulerAdapter = scheduling.SchedulerAdapter
type ScheduledCaptureIO = scheduling.ScheduledCaptureIO
type ScheduledRunnerConfig = scheduling.ScheduledRunnerConfig

func NewScheduledCaptureIO() *ScheduledCaptureIO {
	return scheduling.NewScheduledCaptureIO()
}

func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return scheduling.WithScheduler(s)
}

func RegisterSchedulerTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return scheduling.RegisterSchedulerTools(k, sched)
}

func RegisterSchedulerToolRegistry(reg tool.Registry, sched *scheduler.Scheduler) error {
	return scheduling.RegisterSchedulerToolRegistry(reg, sched)
}

func StartScheduledRunner(ctx context.Context, cfg ScheduledRunnerConfig) error {
	return scheduling.StartScheduledRunner(ctx, cfg)
}

func RunScheduledJob(ctx context.Context, cfg ScheduledRunnerConfig, job scheduler.Job) (*session.Session, *session.LifecycleResult, error) {
	return scheduling.RunScheduledJob(ctx, cfg, job)
}
