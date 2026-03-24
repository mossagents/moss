package builtins

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mossagi/moss/kernel/middleware"
)

// Logger 构造日志 middleware，记录每个 phase 的开始/结束/耗时。
func Logger(w io.Writer) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		start := time.Now()

		label := string(mc.Phase)
		if mc.Tool != nil {
			label += ":" + mc.Tool.Name
		}

		fmt.Fprintf(w, "[%s] %s session=%s start\n", start.Format(time.RFC3339), label, mc.Session.ID)

		err := next(ctx)

		elapsed := time.Since(start)
		if err != nil {
			fmt.Fprintf(w, "[%s] %s session=%s error=%v elapsed=%s\n",
				time.Now().Format(time.RFC3339), label, mc.Session.ID, err, elapsed)
		} else {
			fmt.Fprintf(w, "[%s] %s session=%s done elapsed=%s\n",
				time.Now().Format(time.RFC3339), label, mc.Session.ID, elapsed)
		}

		return err
	}
}
