package hooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Hook 是面向特定生命周期阶段的类型安全回调。
// 返回 error 将中止当前 pipeline。
type Hook[T any] func(ctx context.Context, ev *T) error

// Interceptor 以洋葱模型包裹后续执行。
// 调用 next 继续 pipeline；不调用 next 则短路。
type Interceptor[T any] func(ctx context.Context, ev *T, next func(context.Context) error) error

type entry[T any] struct {
	name        string
	order       int
	hook        Hook[T]
	interceptor Interceptor[T]
	dependsOn   []string
}

// Pipeline 管理某一生命周期阶段的有序 hook/interceptor 列表。
// 执行时按 order 排序，相同 order 保持注册顺序。
type Pipeline[T any] struct {
	mu      sync.RWMutex
	entries []entry[T]
	names   map[string]bool
}

// NewPipeline 创建空 Pipeline。
func NewPipeline[T any]() *Pipeline[T] {
	return &Pipeline[T]{names: make(map[string]bool)}
}

// OnNamed 注册一个命名 hook，支持显式排序和依赖声明。
func (p *Pipeline[T]) OnNamed(name string, order int, hook Hook[T], dependsOn ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, dep := range dependsOn {
		if !p.names[dep] {
			return fmt.Errorf("hook %q depends on %q which is not registered", name, dep)
		}
	}
	p.entries = append(p.entries, entry[T]{name: name, order: order, hook: hook, dependsOn: dependsOn})
	if name != "" {
		p.names[name] = true
	}
	return nil
}

// Intercept 注册一个洋葱式拦截器。
func (p *Pipeline[T]) Intercept(interceptor Interceptor[T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, entry[T]{interceptor: interceptor})
}

// InterceptNamed 注册一个命名拦截器，支持显式排序和依赖声明。
func (p *Pipeline[T]) InterceptNamed(name string, order int, interceptor Interceptor[T], dependsOn ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, dep := range dependsOn {
		if !p.names[dep] {
			return fmt.Errorf("interceptor %q depends on %q which is not registered", name, dep)
		}
	}
	p.entries = append(p.entries, entry[T]{name: name, order: order, interceptor: interceptor, dependsOn: dependsOn})
	if name != "" {
		p.names[name] = true
	}
	return nil
}

// Run 按 order 排序后以洋葱模型执行所有已注册的 hook/interceptor。
func (p *Pipeline[T]) Run(ctx context.Context, ev *T) error {
	p.mu.RLock()
	sorted := make([]entry[T], len(p.entries))
	copy(sorted, p.entries)
	p.mu.RUnlock()

	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].order < sorted[j].order
	})

	return execute(ctx, ev, sorted, 0)
}

func execute[T any](ctx context.Context, ev *T, entries []entry[T], index int) error {
	if index >= len(entries) {
		return nil
	}
	e := entries[index]
	if e.interceptor != nil {
		return e.interceptor(ctx, ev, func(ctx context.Context) error {
			return execute(ctx, ev, entries, index+1)
		})
	}
	if err := e.hook(ctx, ev); err != nil {
		return err
	}
	return execute(ctx, ev, entries, index+1)
}

// AddHook 注册一个命名 hook，支持排序。便捷方法，封装 OnNamed。
func (p *Pipeline[T]) AddHook(name string, hook Hook[T], order int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, entry[T]{name: name, order: order, hook: hook})
	if name != "" {
		p.names[name] = true
	}
}

// AddInterceptor 注册一个命名拦截器，支持排序。便捷方法，封装 InterceptNamed。
func (p *Pipeline[T]) AddInterceptor(name string, interceptor Interceptor[T], order int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, entry[T]{name: name, order: order, interceptor: interceptor})
	if name != "" {
		p.names[name] = true
	}
}

// Empty 返回 pipeline 是否未注册任何 hook。
func (p *Pipeline[T]) Empty() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries) == 0
}
