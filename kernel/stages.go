package kernel

import (
	"context"
	"sort"
	"sync"
)

type orderedBootHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

type orderedShutdownHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

// StageRegistry manages ordered kernel boot and shutdown hooks.
type StageRegistry struct {
	mu sync.RWMutex

	bootHooks     []orderedBootHook
	shutdownHooks []orderedShutdownHook
	bootStarted   bool
	bootCompleted bool
}

func newStageRegistry() *StageRegistry { return &StageRegistry{} }

func (r *StageRegistry) OnBoot(order int, hook func(context.Context, *Kernel) error) {
	if r == nil || hook == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bootHooks = append(r.bootHooks, orderedBootHook{order: order, run: hook})
}

func (r *StageRegistry) OnShutdown(order int, hook func(context.Context, *Kernel) error) {
	if r == nil || hook == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shutdownHooks = append(r.shutdownHooks, orderedShutdownHook{order: order, run: hook})
}

func (r *StageRegistry) BootStarted() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bootStarted
}

func (r *StageRegistry) BootCompleted() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bootCompleted
}

func (r *StageRegistry) runBoot(ctx context.Context, k *Kernel) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.bootStarted = true
	hooks := append([]orderedBootHook(nil), r.bootHooks...)
	r.mu.Unlock()

	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if err := hook.run(ctx, k); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.bootCompleted = true
	r.mu.Unlock()
	return nil
}

func (r *StageRegistry) runShutdown(ctx context.Context, k *Kernel) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	hooks := append([]orderedShutdownHook(nil), r.shutdownHooks...)
	r.mu.RUnlock()

	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if err := hook.run(ctx, k); err != nil {
			return err
		}
	}
	return nil
}
