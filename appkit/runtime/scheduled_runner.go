package runtime

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/scheduler"
)

type ScheduledRunnerConfig struct {
	Kernel             *kernel.Kernel
	Scheduler          *scheduler.Scheduler
	SessionStore       session.SessionStore
	DefaultIO          port.UserIO
	BuildSessionConfig func(context.Context, scheduler.Job) (session.SessionConfig, error)
	RunIO              func(context.Context, scheduler.Job) port.UserIO
	BeforeRun          func(context.Context, scheduler.Job)
	OnPrepareError     func(context.Context, scheduler.Job, error)
	OnCreateError      func(context.Context, scheduler.Job, error)
	OnRunError         func(context.Context, scheduler.Job, *session.Session, error, port.UserIO)
	OnSaveError        func(context.Context, scheduler.Job, *session.Session, error)
	OnComplete         func(context.Context, scheduler.Job, *session.Session, *loop.SessionResult, port.UserIO)
}

func StartScheduledRunner(ctx context.Context, cfg ScheduledRunnerConfig) error {
	if cfg.Kernel == nil {
		return fmt.Errorf("kernel is nil")
	}
	if cfg.Scheduler == nil {
		return fmt.Errorf("scheduler is nil")
	}
	if cfg.BuildSessionConfig == nil {
		return fmt.Errorf("BuildSessionConfig is nil")
	}
	cfg.Scheduler.Start(ctx, func(jobCtx context.Context, job scheduler.Job) {
		_, _, _ = RunScheduledJob(jobCtx, cfg, job)
	})
	return nil
}

func RunScheduledJob(ctx context.Context, cfg ScheduledRunnerConfig, job scheduler.Job) (*session.Session, *loop.SessionResult, error) {
	if cfg.BeforeRun != nil {
		cfg.BeforeRun(ctx, job)
	}

	jobCfg, err := cfg.BuildSessionConfig(ctx, job)
	if err != nil {
		if cfg.OnPrepareError != nil {
			cfg.OnPrepareError(ctx, job, err)
		}
		return nil, nil, err
	}
	if jobCfg.Goal == "" {
		jobCfg.Goal = job.Goal
	}
	if jobCfg.Mode == "" {
		jobCfg.Mode = "scheduled"
	}

	jobSess, err := cfg.Kernel.NewSession(ctx, jobCfg)
	if err != nil {
		if cfg.OnCreateError != nil {
			cfg.OnCreateError(ctx, job, err)
		}
		return nil, nil, err
	}

	jobSess.AppendMessage(port.Message{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart(job.Goal)}})

	runIO := cfg.DefaultIO
	if cfg.RunIO != nil {
		if override := cfg.RunIO(ctx, job); override != nil {
			runIO = override
		}
	}

	var result *loop.SessionResult
	if runIO != nil {
		result, err = cfg.Kernel.RunWithUserIO(ctx, jobSess, runIO)
	} else {
		result, err = cfg.Kernel.Run(ctx, jobSess)
	}
	if err != nil {
		if cfg.OnRunError != nil {
			cfg.OnRunError(ctx, job, jobSess, err, runIO)
		}
		return jobSess, nil, err
	}

	if cfg.SessionStore != nil {
		if saveErr := cfg.SessionStore.Save(ctx, jobSess); saveErr != nil && cfg.OnSaveError != nil {
			cfg.OnSaveError(ctx, job, jobSess, saveErr)
		}
	}
	if cfg.OnComplete != nil {
		cfg.OnComplete(ctx, job, jobSess, result, runIO)
	}
	return jobSess, result, nil
}
