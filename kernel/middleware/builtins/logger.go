package builtins

import (
	"context"
	"github.com/mossagents/moss/kernel/middleware"
	"log/slog"
	"time"
)

// Logger 构造日志 middleware，记录每个 phase 的开始/结束/耗时。
// 使用 slog 输出结构化日志。
func Logger() middleware.Middleware {
	logger := slog.Default()
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		start := time.Now()

		label := string(mc.Phase)
		if mc.Tool != nil {
			label += ":" + mc.Tool.Name
		}

		logger.InfoContext(ctx, "phase start",
			slog.String("phase", label),
			slog.String("session_id", mc.Session.ID),
		)

		err := next(ctx)

		elapsed := time.Since(start)
		if err != nil {
			logger.ErrorContext(ctx, "phase error",
				slog.String("phase", label),
				slog.String("session_id", mc.Session.ID),
				slog.Duration("elapsed", elapsed),
				slog.Any("error", err),
			)
		} else {
			logger.InfoContext(ctx, "phase done",
				slog.String("phase", label),
				slog.String("session_id", mc.Session.ID),
				slog.Duration("elapsed", elapsed),
			)
		}

		return err
	}
}
