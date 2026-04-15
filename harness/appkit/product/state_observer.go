package product

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/harness/runtime"
)

// ComposeStateObserver 将 state catalog observer 与现有 observer 组合。
func ComposeStateObserver(k *kernel.Kernel, base observe.Observer) observe.Observer {
	return observe.JoinObservers(base, runtime.ObserverForStateCatalog(k))
}
