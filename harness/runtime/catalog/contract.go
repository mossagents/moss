package catalog

import "github.com/mossagents/moss/kernel"

const ServiceKey kernel.ServiceKey = "statecatalog.view"

type Kind string

const (
	KindCheckpoint     Kind = "checkpoint"
	KindTask           Kind = "task"
	KindJob            Kind = "job"
	KindJobItem        Kind = "job_item"
	KindMemory         Kind = "memory"
	KindExecutionEvent Kind = "execution_event"
)

type Entry struct {
	Kind     Kind
	Status   string
	Title    string
	Summary  string
	RecordID string
}

type Query struct {
	Kinds     []Kind
	SessionID string
	Limit     int
}

type Page struct {
	Items         []Entry
	NextCursor    string
	TotalEstimate int
}

type Catalog interface {
	Enabled() bool
	Query(Query) (Page, error)
}

func WithCatalog(c Catalog) kernel.Option {
	return func(k *kernel.Kernel) {
		if k == nil {
			return
		}
		k.Services().Store(ServiceKey, c)
	}
}

func Lookup(k *kernel.Kernel) Catalog {
	if k == nil {
		return nil
	}
	actual, ok := k.Services().Load(ServiceKey)
	if !ok || actual == nil {
		return nil
	}
	catalog, _ := actual.(Catalog)
	return catalog
}
