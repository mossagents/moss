package hooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

// DispatchSessionLifecycle is the single dispatcher for session lifecycle events.
// It fan-outs to SessionObserver first, then to the hook pipeline, and reports
// hook errors/panics through the shared observe.Error path.
func DispatchSessionLifecycle(ctx context.Context, reg *Registry, observer observe.Observer, logger *slog.Logger, event session.LifecycleEvent) {
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if observer == nil {
		observer = observe.NoOpObserver{}
	}
	if logger == nil {
		logger = slog.Default()
	}

	sessionID := ""
	if event.Session != nil {
		sessionID = event.Session.ID
	}
	observe.ObserveSessionEvent(callCtx, observer, observe.SessionEvent{
		SessionID: sessionID,
		Type:      lifecycleObserverType(event.Stage),
	})

	if reg == nil || reg.OnSessionLifecycle == nil {
		return
	}

	reportErr := func(err error) {
		logger.ErrorContext(callCtx, "session lifecycle hook error",
			slog.String("stage", string(event.Stage)),
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		observe.ObserveError(callCtx, observer, observe.ErrorEvent{
			SessionID: sessionID,
			Phase:     "session_lifecycle_hook",
			Error:     err,
			Message:   err.Error(),
		})
	}

	defer func() {
		if r := recover(); r != nil {
			reportErr(fmt.Errorf("session lifecycle hook panic: %v", r))
		}
	}()

	if err := reg.OnSessionLifecycle.Run(callCtx, &event); err != nil {
		reportErr(err)
	}
}

func lifecycleObserverType(stage session.LifecycleStage) string {
	switch stage {
	case session.LifecycleCreated:
		return "created"
	case session.LifecycleStarted:
		return "running"
	case session.LifecycleFailed:
		return "failed"
	case session.LifecycleCancelled:
		return "cancelled"
	default:
		return "completed"
	}
}
