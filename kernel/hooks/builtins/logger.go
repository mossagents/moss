package builtins

import (
	"context"
	"log/slog"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// InstallLogger 安装日志 hooks，在每个 pipeline 上记录 start/done/error。
// Logger 是跨 pipeline 的安装器，而非单个 hook。
func InstallLogger(reg *hooks.Registry) {
	logger := slog.Default()

	reg.BeforeLLM.AddInterceptor("logger", logInterceptor(logger, "before_llm", func(ev *hooks.LLMEvent) string {
		if ev.Session != nil {
			return ev.Session.ID
		}
		return ""
	}), 1000) // 高 order 值确保 logger 在最外层

	reg.AfterLLM.AddInterceptor("logger", logInterceptor(logger, "after_llm", func(ev *hooks.LLMEvent) string {
		if ev.Session != nil {
			return ev.Session.ID
		}
		return ""
	}), 1000)

	reg.OnToolLifecycle.AddInterceptor("logger", logToolInterceptor(logger), 1000)
	reg.OnSessionLifecycle.AddInterceptor("logger", logSessionInterceptor(logger), 1000)

	reg.OnError.AddHook("logger", func(ctx context.Context, ev *hooks.ErrorEvent) error {
		logger.ErrorContext(ctx, "hook error",
			slog.String("phase", "on_error"),
			slog.Any("error", ev.Error),
		)
		return nil
	}, 1000)
}

func logInterceptor[T any](logger *slog.Logger, phase string, sessionID func(*T) string) hooks.Interceptor[T] {
	return func(ctx context.Context, ev *T, next func(context.Context) error) error {
		start := time.Now()
		sid := sessionID(ev)
		logger.InfoContext(ctx, "hook start",
			slog.String("phase", phase),
			slog.String("session_id", sid),
		)
		err := next(ctx)
		elapsed := time.Since(start)
		if err != nil {
			logger.ErrorContext(ctx, "hook error",
				slog.String("phase", phase),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
				slog.Any("error", err),
			)
		} else {
			logger.InfoContext(ctx, "hook done",
				slog.String("phase", phase),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
			)
		}
		return err
	}
}

func logToolInterceptor(logger *slog.Logger) hooks.Interceptor[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent, next func(context.Context) error) error {
		start := time.Now()
		label := "tool_lifecycle"
		if ev != nil {
			label += ":" + string(ev.Stage)
			if ev.Tool != nil && ev.Tool.Name != "" {
				label += ":" + ev.Tool.Name
			} else if ev.ToolName != "" {
				label += ":" + ev.ToolName
			}
		}
		sid := ""
		if ev != nil && ev.Session != nil {
			sid = ev.Session.ID
		}
		logger.InfoContext(ctx, "hook start",
			slog.String("phase", label),
			slog.String("session_id", sid),
		)
		err := next(ctx)
		elapsed := time.Since(start)
		if err != nil {
			logger.ErrorContext(ctx, "hook error",
				slog.String("phase", label),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
				slog.Any("error", err),
			)
		} else {
			logger.InfoContext(ctx, "hook done",
				slog.String("phase", label),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
			)
		}
		return err
	}
}

func logSessionInterceptor(logger *slog.Logger) hooks.Interceptor[session.LifecycleEvent] {
	return func(ctx context.Context, ev *session.LifecycleEvent, next func(context.Context) error) error {
		start := time.Now()
		phase := "session_lifecycle"
		if ev != nil {
			phase += ":" + string(ev.Stage)
		}
		sid := ""
		if ev != nil && ev.Session != nil {
			sid = ev.Session.ID
		}
		logger.InfoContext(ctx, "hook start",
			slog.String("phase", phase),
			slog.String("session_id", sid),
		)
		err := next(ctx)
		elapsed := time.Since(start)
		if err != nil {
			logger.ErrorContext(ctx, "hook error",
				slog.String("phase", phase),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
				slog.Any("error", err),
			)
		} else {
			logger.InfoContext(ctx, "hook done",
				slog.String("phase", phase),
				slog.String("session_id", sid),
				slog.Duration("elapsed", elapsed),
			)
		}
		return err
	}
}
