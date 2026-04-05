package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

const sessionStoreStateKey kernel.ExtensionStateKey = "sessionstore.state"

type sessionStoreState struct {
	store session.SessionStore
}

// WithKernelSessionStore attaches a SessionStore via extension bridge hooks.
func WithKernelSessionStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		kernel.WithSessionStore(store)(k)
		ensureSessionStoreState(k).store = store
	}
}

func ensureSessionStoreState(k *kernel.Kernel) *sessionStoreState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(sessionStoreStateKey, &sessionStoreState{})
	st := actual.(*sessionStoreState)
	if loaded {
		return st
	}
	bridge.OnShutdown(100, func(ctx context.Context, k *kernel.Kernel) error {
		if st.store == nil {
			return nil
		}
		for _, sess := range k.SessionManager().List() {
			if sess.Status == session.StatusRunning {
				sess.Status = session.StatusPaused
			}
			if err := st.store.Save(ctx, sess); err != nil {
				return fmt.Errorf("save session %q during shutdown: %w", sess.ID, err)
			}
		}
		return nil
	})
	bridge.OnSessionLifecycle(100, func(ctx context.Context, event session.LifecycleEvent) {
		persistSessionEvent(ctx, st.store, event.Session, event.Timestamp, string(event.Stage))
	})
	bridge.OnToolLifecycle(100, func(ctx context.Context, event session.ToolLifecycleEvent) {
		if event.Stage != session.ToolLifecycleAfter {
			return
		}
		persistSessionEvent(ctx, st.store, event.Session, event.Timestamp, "tool:"+strings.TrimSpace(event.ToolName))
	})
	return st
}

func persistSessionEvent(ctx context.Context, store session.SessionStore, sess *session.Session, when time.Time, kind string) {
	if store == nil || sess == nil {
		return
	}
	session.RefreshThreadMetadata(sess, when, kind)
	if err := store.Save(ctx, sess); err != nil {
		logging.GetLogger().WarnContext(ctx, "persist session event failed", "session_id", sess.ID, "kind", kind, "error", err)
	}
}
