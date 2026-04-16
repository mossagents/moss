package plugin

import (
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// Group 是一个 Plugin 构建器，通过方法链收集 hook/interceptor 注册，
// 适用于单阶段或简单多阶段的插件场景。
type Group struct {
	name  string
	order int
	steps []func(reg *hooks.Registry)
}

// NewGroup 创建一个 Group。
func NewGroup(name string, order int) *Group {
	return &Group{name: name, order: order}
}

func (g *Group) Name() string { return g.name }
func (g *Group) Order() int   { return g.order }
func (g *Group) Empty() bool  { return len(g.steps) == 0 }

func (g *Group) Install(reg *hooks.Registry) {
	for _, step := range g.steps {
		step(reg)
	}
}

// ── Hook 注册 ───────────────────────────────────────────────────

func (g *Group) OnBeforeLLM(h hooks.Hook[hooks.LLMEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.BeforeLLM.AddHook(g.name, h, g.order)
	})
}

func (g *Group) OnAfterLLM(h hooks.Hook[hooks.LLMEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.AfterLLM.AddHook(g.name, h, g.order)
	})
}

func (g *Group) OnToolLifecycle(h hooks.Hook[hooks.ToolEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnToolLifecycle.AddHook(g.name, h, g.order)
	})
}

func (g *Group) OnSessionLifecycle(h hooks.Hook[session.LifecycleEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnSessionLifecycle.AddHook(g.name, h, g.order)
	})
}

func (g *Group) OnError(h hooks.Hook[hooks.ErrorEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnError.AddHook(g.name, h, g.order)
	})
}

// ── Interceptor 注册 ────────────────────────────────────────────

func (g *Group) InterceptBeforeLLM(i hooks.Interceptor[hooks.LLMEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.BeforeLLM.AddInterceptor(g.name, i, g.order)
	})
}

func (g *Group) InterceptAfterLLM(i hooks.Interceptor[hooks.LLMEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.AfterLLM.AddInterceptor(g.name, i, g.order)
	})
}

func (g *Group) InterceptToolLifecycle(i hooks.Interceptor[hooks.ToolEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnToolLifecycle.AddInterceptor(g.name, i, g.order)
	})
}

func (g *Group) InterceptSessionLifecycle(i hooks.Interceptor[session.LifecycleEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnSessionLifecycle.AddInterceptor(g.name, i, g.order)
	})
}

func (g *Group) InterceptError(i hooks.Interceptor[hooks.ErrorEvent]) {
	g.steps = append(g.steps, func(reg *hooks.Registry) {
		reg.OnError.AddInterceptor(g.name, i, g.order)
	})
}
