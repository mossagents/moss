package runtime

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/observe"
)

const stateCatalogStateKey = kernel.ServiceKey("statecatalog.state")

type stateCatalogState struct {
	catalog *StateCatalog
}

func WithStateCatalog(catalog *StateCatalog) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureStateCatalogState(k).catalog = catalog
	}
}

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

func ensureStateCatalogState(k *kernel.Kernel) *stateCatalogState {
	actual, loaded := k.Services().LoadOrStore(stateCatalogStateKey, &stateCatalogState{})
	state := actual.(*stateCatalogState)
	if loaded {
		return state
	}
	return state
}
