package kernel

import (
	"sort"
	"sync"
)

type orderedPromptHook struct {
	order int
	run   func(*Kernel) string
}

// PromptAssembler manages ordered system-prompt fragment builders.
type PromptAssembler struct {
	mu    sync.RWMutex
	hooks []orderedPromptHook
}

func newPromptAssembler() *PromptAssembler { return &PromptAssembler{} }

func (a *PromptAssembler) Add(order int, hook func(*Kernel) string) {
	if a == nil || hook == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hooks = append(a.hooks, orderedPromptHook{order: order, run: hook})
}

func (a *PromptAssembler) Extend(k *Kernel, base string) string {
	if a == nil {
		return base
	}
	sysPrompt := base
	a.mu.RLock()
	hooks := append([]orderedPromptHook(nil), a.hooks...)
	a.mu.RUnlock()

	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if section := hook.run(k); section != "" {
			if sysPrompt != "" {
				sysPrompt += "\n\n" + section
			} else {
				sysPrompt = section
			}
		}
	}
	return sysPrompt
}
