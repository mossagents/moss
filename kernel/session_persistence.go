package kernel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	kplugin "github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
)

const persistentSessionStoreStateKey ServiceKey = "persistent-session-store.state"

type persistentSessionStoreState struct {
	store session.SessionStore
}

// WithPersistentSessionStore attaches a SessionStore and installs kernel-managed
// persistence hooks. Public feature composition should prefer harness.SessionPersistence.
//
// Deprecated: 阶段 4 将删除本选项。
// 新路径请改用 kernel.WithEventStore + kernel.StartRuntimeSession，
// 以 EventStore 事件流为事实来源。
func WithPersistentSessionStore(store session.SessionStore) Option {
	return func(k *Kernel) {
		WithSessionStore(store)(k)
		ensurePersistentSessionStoreState(k).store = store
	}
}

// ensurePersistentSessionStoreState owns the persistent-session substrate slot.
func ensurePersistentSessionStoreState(k *Kernel) *persistentSessionStoreState {
	actual, loaded := k.Services().LoadOrStore(persistentSessionStoreStateKey, &persistentSessionStoreState{})
	st := actual.(*persistentSessionStoreState)
	if loaded {
		return st
	}
	if err := k.Stages().OnShutdown(100, func(ctx context.Context, k *Kernel) error {
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
	}); err != nil {
		slog.Default().Warn("failed to register shutdown hook for session persistence", "error", err)
	}
	g := kplugin.NewGroup("persistent-session-store", 100)
	g.OnSessionLifecycle(func(ctx context.Context, event *session.LifecycleEvent) error {
		if event == nil {
			return nil
		}
		persistPersistentSessionEvent(ctx, st.store, event.Session, event.Timestamp, string(event.Stage))
		return nil
	})
	g.OnToolLifecycle(func(ctx context.Context, event *hooks.ToolEvent) error {
		if event == nil || event.Stage != hooks.ToolLifecycleAfter {
			return nil
		}
		persistPersistentSessionEvent(ctx, st.store, event.Session, event.Timestamp, "tool:"+strings.TrimSpace(event.ToolName))
		return nil
	})
	k.installPlugin(g)
	return st
}

func persistPersistentSessionEvent(ctx context.Context, store session.SessionStore, sess *session.Session, when time.Time, kind string) {
	if store == nil || sess == nil {
		return
	}
	session.RefreshThreadMetadata(sess, when, kind)
	if err := store.Save(ctx, sess); err != nil {
		slog.Default().WarnContext(ctx, "persist session event failed", "session_id", sess.ID, "kind", kind, "error", err)
	}
}
