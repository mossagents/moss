package kernel

import (
	"context"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"log/slog"
	"sort"
	"sync"
)

// ExtensionStateKey 标识一个扩展状态槽。
//
// ExtensionBridge 管理 Kernel 级扩展的生命周期钩子（OnBoot、OnShutdown、OnSystemPrompt、
// OnSessionStart、OnToolCall）。它与 hooks.Registry 分工明确：
//   - ExtensionBridge：Kernel 启动/关停、System Prompt 组装等初始化阶段
//   - hooks.Registry：Agent Loop 运行时的 LLM/Tool 调用钩子
type ExtensionStateKey string

type extensionState struct {
	mu sync.RWMutex

	values map[ExtensionStateKey]any

	bootHooks     []orderedBootHook
	shutdownHooks []orderedShutdownHook
	promptHooks   []orderedPromptHook
	sessionHooks  []orderedSessionHook
	toolHooks     []orderedToolHook
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

type orderedSessionHook struct {
	order int
	run   session.LifecycleHook
}

type orderedToolHook struct {
	order int
	run   session.ToolLifecycleHook
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

func (k *Kernel) sessionLifecycleHooks() []orderedSessionHook {
	ext := k.extensionState()
	ext.mu.RLock()
	hooks := append([]orderedSessionHook(nil), ext.sessionHooks...)
	ext.mu.RUnlock()

	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	return hooks
}

func (k *Kernel) emitSessionLifecycle(ctx context.Context, event session.LifecycleEvent) {
	for _, hook := range k.sessionLifecycleHooks() {
		if hook.run == nil {
			continue
		}
		k.runSessionLifecycleHook(ctx, hook.run, event)
	}
}

func (k *Kernel) toolLifecycleHooks() []orderedToolHook {
	ext := k.extensionState()
	ext.mu.RLock()
	hooks := append([]orderedToolHook(nil), ext.toolHooks...)
	ext.mu.RUnlock()

	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	return hooks
}

func (k *Kernel) emitToolLifecycle(ctx context.Context, event session.ToolLifecycleEvent) {
	for _, hook := range k.toolLifecycleHooks() {
		if hook.run == nil {
			continue
		}
		k.runToolLifecycleHook(ctx, hook.run, event)
	}
}

func (k *Kernel) runSessionLifecycleHook(ctx context.Context, hook session.LifecycleHook, event session.LifecycleEvent) {
	defer func() {
		if r := recover(); r != nil {
			sessionID := ""
			if event.Session != nil {
				sessionID = event.Session.ID
			}
			err := panicAsError("session lifecycle hook panic", r)
			slog.Default().ErrorContext(contextOrBackground(ctx), "session lifecycle hook panic",
				slog.String("stage", string(event.Stage)),
				slog.String("session_id", sessionID),
				slog.Any("panic", r),
			)
			observe.ObserveError(contextOrBackground(ctx), k.observerOrNoOp(), observe.ErrorEvent{
				SessionID: sessionID,
				Phase:     "session_lifecycle_hook",
				Error:     err,
				Message:   err.Error(),
			})
		}
	}()
	hook(contextOrBackground(ctx), event)
}

func (k *Kernel) runToolLifecycleHook(ctx context.Context, hook session.ToolLifecycleHook, event session.ToolLifecycleEvent) {
	defer func() {
		if r := recover(); r != nil {
			sessionID := ""
			if event.Session != nil {
				sessionID = event.Session.ID
			}
			err := panicAsError("tool lifecycle hook panic", r)
			slog.Default().ErrorContext(contextOrBackground(ctx), "tool lifecycle hook panic",
				slog.String("stage", string(event.Stage)),
				slog.String("session_id", sessionID),
				slog.String("tool", event.ToolName),
				slog.String("call_id", event.CallID),
				slog.Any("panic", r),
			)
			observe.ObserveError(contextOrBackground(ctx), k.observerOrNoOp(), observe.ErrorEvent{
				SessionID: sessionID,
				Phase:     "tool_lifecycle_hook",
				Error:     err,
				Message:   err.Error(),
			})
		}
	}()
	hook(contextOrBackground(ctx), event)
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

// OnSessionLifecycle 注册一个按顺序执行的 Session 生命周期 hook。
func (b *ExtensionBridge) OnSessionLifecycle(order int, hook session.LifecycleHook) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.sessionHooks = append(ext.sessionHooks, orderedSessionHook{
		order: order,
		run:   hook,
	})
}

// OnToolLifecycle 注册一个按顺序执行的工具调用生命周期 hook。
func (b *ExtensionBridge) OnToolLifecycle(order int, hook session.ToolLifecycleHook) {
	ext := b.k.extensionState()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.toolHooks = append(ext.toolHooks, orderedToolHook{
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
