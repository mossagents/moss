package product

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
)

// ComposeStateObserver 将 state catalog observer 与现有 observer 组合。
func ComposeStateObserver(k *kernel.Kernel, base port.Observer) port.Observer {
	return port.JoinObservers(base, runtime.ObserverForStateCatalog(k))
}
