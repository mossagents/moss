package kernel

import (
	"context"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// emitSessionLifecycle dispatches session lifecycle through the shared
// observer + hook dispatcher for consistent reporting.
func (k *Kernel) emitSessionLifecycle(ctx context.Context, event session.LifecycleEvent) {
	hooks.DispatchSessionLifecycle(contextOrBackground(ctx), k.chain, k.observerOrNoOp(), k.Logger(), event)
}
