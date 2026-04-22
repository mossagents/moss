package kernel

import (
	"context"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// emitSessionLifecycle dispatches session lifecycle through the shared
// observer + hook dispatcher so legacy and runtime-backed paths follow the
// same reporting contract.
func (k *Kernel) emitSessionLifecycle(ctx context.Context, event session.LifecycleEvent) {
	hooks.DispatchSessionLifecycle(contextOrBackground(ctx), k.chain, k.observerOrNoOp(), k.Logger(), event)
}
