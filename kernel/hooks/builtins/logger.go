package builtins

import (
	"context"
	"log/slog"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	kplugin "github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
)

// LoggerPlugin returns the canonical logger lifecycle plugin.
func LoggerPlugin() kplugin.Plugin {
	logger := slog.Default()
	return kplugin.Plugin{
		Name:                          "logger",
		Order:                         1000,
		BeforeLLMInterceptor:          logInterceptor(logger, "before_llm", llmSessionID),
		AfterLLMInterceptor:           logInterceptor(logger, "after_llm", llmSessionID),
		OnToolLifecycleInterceptor:    logToolInterceptor(logger),
		OnSessionLifecycleInterceptor: logSessionInterceptor(logger),
		OnError: func(ctx context.Context, ev *hooks.ErrorEvent) error {
			logger.ErrorContext(ctx, "hook error",
				slog.String("phase", "on_error"),
				slog.Any("error", ev.Error),
			)
			return nil
		},
	}
}

func llmSessionID(ev *hooks.LLMEvent) string {
	if ev.Session != nil {
		return ev.Session.ID
	}
	return ""
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
