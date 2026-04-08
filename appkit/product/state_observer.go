package product

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	kobs "github.com/mossagents/moss/kernel/observe"
)

// ComposeStateObserver 将 state catalog observer 与现有 observer 组合。
func ComposeStateObserver(k *kernel.Kernel, base kobs.Observer) kobs.Observer {
	return kobs.JoinObservers(base, runtime.ObserverForStateCatalog(k))
}
