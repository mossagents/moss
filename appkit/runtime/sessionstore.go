package runtime

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
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
	return st
}
