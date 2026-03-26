package kernel

import (
	"context"
	"sort"
	"sync"
)

// ExtensionStateKey 标识一个扩展状态槽。
type ExtensionStateKey string

type extensionState struct {
	mu sync.RWMutex

	values map[ExtensionStateKey]any

	bootHooks     []orderedBootHook
	shutdownHooks []orderedShutdownHook
	promptHooks   []orderedPromptHook
}

type orderedBootHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

type orderedShutdownHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

type orderedPromptHook struct {
	order int
	run   func(*Kernel) string
}

func newExtensionState() *extensionState {
	return &extensionState{
		values: make(map[ExtensionStateKey]any),
	}
}

// ExtensionBridge 提供扩展层对非 core 生命周期与状态槽的通用接入入口。
type ExtensionBridge struct {
	k *Kernel
}

// Extensions 返回扩展桥接配置入口。
func Extensions(k *Kernel) *ExtensionBridge {
	return &ExtensionBridge{k: k}
}

func (k *Kernel) extensionState() *extensionState {
	if k.ext == nil {
		k.ext = newExtensionState()
	}
	return k.ext
}

func (k *Kernel) bootExtensions(ctx context.Context) error {
	ext := k.extensionState()
	ext.mu.RLock()
	hooks := append([]orderedBootHook(nil), ext.bootHooks...)
	ext.mu.RUnlock()

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

func (k *Kernel) shutdownExtensions(ctx context.Context) error {
	ext := k.extensionState()
	ext.mu.RLock()
	hooks := append([]orderedShutdownHook(nil), ext.shutdownHooks...)
	ext.mu.RUnlock()

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

func (k *Kernel) extendSystemPrompt(base string) string {
	sysPrompt := base

	ext := k.extensionState()
	ext.mu.RLock()
	hooks := append([]orderedPromptHook(nil), ext.promptHooks...)
	ext.mu.RUnlock()

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

// OnBoot 注册一个按顺序执行的扩展启动 hook。
func (b *ExtensionBridge) OnBoot(order int, hook func(context.Context, *Kernel) error) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.bootHooks = append(ext.bootHooks, orderedBootHook{
		order: order,
		run:   hook,
	})
}

// OnShutdown 注册一个按顺序执行的扩展关停 hook。
func (b *ExtensionBridge) OnShutdown(order int, hook func(context.Context, *Kernel) error) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.shutdownHooks = append(ext.shutdownHooks, orderedShutdownHook{
		order: order,
		run:   hook,
	})
}

// OnSystemPrompt 注册一个系统提示词增强 hook。
func (b *ExtensionBridge) OnSystemPrompt(order int, hook func(*Kernel) string) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.promptHooks = append(ext.promptHooks, orderedPromptHook{
		order: order,
		run:   hook,
	})
}

// State 返回指定 key 的扩展状态。
func (b *ExtensionBridge) State(key ExtensionStateKey) (any, bool) {
	ext := b.k.extensionState()
	ext.mu.RLock()
	defer ext.mu.RUnlock()
	value, ok := ext.values[key]
	return value, ok
}

// SetState 写入指定 key 的扩展状态。
func (b *ExtensionBridge) SetState(key ExtensionStateKey, value any) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.values[key] = value
}

// LoadOrStoreState 返回已有状态；若不存在则存入给定值并返回该值。
// 返回值 loaded 表示是否命中了已有状态。
func (b *ExtensionBridge) LoadOrStoreState(key ExtensionStateKey, value any) (actual any, loaded bool) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if actual, loaded = ext.values[key]; loaded {
		return actual, true
	}
	ext.values[key] = value
	return value, false
}
