package runtime

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/observe"
	statecatalog "github.com/mossagents/moss/harness/runtime/catalog"
	rstate "github.com/mossagents/moss/harness/runtime/state"
)

const stateCatalogStateKey = kernel.ServiceKey("statecatalog.state")

type stateCatalogState struct {
	catalog *rstate.StateCatalog
}

type stateCatalogView struct {
	catalog *rstate.StateCatalog
}

// WithStateCatalog stores the runtime-owned state catalog substrate. Public
// assembly should prefer harness-owned features over direct Services() access.
func WithStateCatalog(catalog *rstate.StateCatalog) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureStateCatalogState(k).catalog = catalog
		statecatalog.WithCatalog(stateCatalogView{catalog: catalog})(k)
	}
}

// StateCatalogOf looks up the runtime-owned state catalog without creating the
// underlying kernel substrate slot on first access.
func StateCatalogOf(k *kernel.Kernel) *rstate.StateCatalog {
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
	return rstate.NewStateCatalogObserver(StateCatalogOf(k))
}

func (v stateCatalogView) Enabled() bool {
	return v.catalog != nil && v.catalog.Enabled()
}

func (v stateCatalogView) Query(query statecatalog.Query) (statecatalog.Page, error) {
	if v.catalog == nil {
		return statecatalog.Page{}, nil
	}
	kinds := make([]rstate.StateKind, 0, len(query.Kinds))
	for _, kind := range query.Kinds {
		kinds = append(kinds, rstate.StateKind(kind))
	}
	page, err := v.catalog.Query(rstate.StateQuery{
		Kinds:     kinds,
		SessionID: query.SessionID,
		Limit:     query.Limit,
	})
	if err != nil {
		return statecatalog.Page{}, err
	}
	items := make([]statecatalog.Entry, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, statecatalog.Entry{
			Kind:     statecatalog.Kind(item.Kind),
			Status:   item.Status,
			Title:    item.Title,
			Summary:  item.Summary,
			RecordID: item.RecordID,
		})
	}
	return statecatalog.Page{
		Items:         items,
		NextCursor:    page.NextCursor,
		TotalEstimate: page.TotalEstimate,
	}, nil
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
