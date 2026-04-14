package kernel

import (
	"context"
	"log/slog"

	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

func (k *Kernel) emitSessionLifecycle(ctx context.Context, event session.LifecycleEvent) {
	if err := k.chain.OnSessionLifecycle.Run(contextOrBackground(ctx), &event); err != nil {
		sessionID := ""
		if event.Session != nil {
			sessionID = event.Session.ID
		}
		slog.Default().ErrorContext(contextOrBackground(ctx), "session lifecycle hook error",
			slog.String("stage", string(event.Stage)),
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		observe.ObserveError(contextOrBackground(ctx), k.observerOrNoOp(), observe.ErrorEvent{
			SessionID: sessionID,
			Phase:     "session_lifecycle_hook",
			Error:     err,
			Message:   err.Error(),
		})
	}
}
