package runtime

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/observe"
)

const stateCatalogStateKey = kernel.ServiceKey("statecatalog.state")

type stateCatalogState struct {
	catalog *StateCatalog
}

// WithStateCatalog stores the runtime-owned state catalog substrate. Public
// assembly should prefer harness-owned features over direct Services() access.
func WithStateCatalog(catalog *StateCatalog) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureStateCatalogState(k).catalog = catalog
	}
}

// StateCatalogOf looks up the runtime-owned state catalog without creating the
// underlying kernel substrate slot on first access.
func StateCatalogOf(k *kernel.Kernel) *StateCatalog {
	if k == nil {
		return nil
	}
	actual, ok := k.Services().Load(stateCatalogStateKey)
	if !ok {
		return nil
	}
	state, _ := actual.(*stateCatalogState)
	if state == nil {
		return nil
	}
	return state.catalog
}

func ObserverForStateCatalog(k *kernel.Kernel) observe.Observer {
	return NewStateCatalogObserver(StateCatalogOf(k))
}

// ensureStateCatalogState owns the runtime state-catalog substrate slot.
func ensureStateCatalogState(k *kernel.Kernel) *stateCatalogState {
	actual, loaded := k.Services().LoadOrStore(stateCatalogStateKey, &stateCatalogState{})
	state := actual.(*stateCatalogState)
	if loaded {
		return state
	}
	return state
}
