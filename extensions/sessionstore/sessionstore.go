package sessionstore

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

const stateKey kernel.ExtensionStateKey = "sessionstore.state"

type state struct {
	store session.SessionStore
}

// WithStore 将 SessionStore 作为标准扩展接入 Kernel。
func WithStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).store = store
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{})
	st := actual.(*state)
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
