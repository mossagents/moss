package kernel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

const persistentSessionStoreStateKey ServiceKey = "persistent-session-store.state"

type persistentSessionStoreState struct {
	store session.SessionStore
}

// WithPersistentSessionStore attaches a SessionStore and installs kernel-managed
// persistence hooks. Public feature composition should prefer harness.SessionPersistence.
func WithPersistentSessionStore(store session.SessionStore) Option {
	return func(k *Kernel) {
		WithSessionStore(store)(k)
		ensurePersistentSessionStoreState(k).store = store
	}
}

func ensurePersistentSessionStoreState(k *Kernel) *persistentSessionStoreState {
	actual, loaded := k.Services().LoadOrStore(persistentSessionStoreStateKey, &persistentSessionStoreState{})
	st := actual.(*persistentSessionStoreState)
	if loaded {
		return st
	}
	k.Stages().OnShutdown(100, func(ctx context.Context, k *Kernel) error {
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
	k.Hooks().OnSessionLifecycle.AddHook("persistent-session-store", func(ctx context.Context, event *session.LifecycleEvent) error {
		if event == nil {
			return nil
		}
		persistPersistentSessionEvent(ctx, st.store, event.Session, event.Timestamp, string(event.Stage))
		return nil
	}, 100)
	k.Hooks().OnToolLifecycle.AddHook("persistent-session-store-tool", func(ctx context.Context, event *session.ToolLifecycleEvent) error {
		if event == nil || event.Stage != session.ToolLifecycleAfter {
			return nil
		}
		persistPersistentSessionEvent(ctx, st.store, event.Session, event.Timestamp, "tool:"+strings.TrimSpace(event.ToolName))
		return nil
	}, 100)
	return st
}

func persistPersistentSessionEvent(ctx context.Context, store session.SessionStore, sess *session.Session, when time.Time, kind string) {
	if store == nil || sess == nil {
		return
	}
	session.RefreshThreadMetadata(sess, when, kind)
	if err := store.Save(ctx, sess); err != nil {
		logging.GetLogger().WarnContext(ctx, "persist session event failed", "session_id", sess.ID, "kind", kind, "error", err)
	}
}
