package plugin

import (
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// ── Hook 便捷构造 ───────────────────────────────────────────────

func BeforeLLMHook(name string, order int, h hooks.Hook[hooks.LLMEvent]) *Group {
	g := NewGroup(name, order)
	g.OnBeforeLLM(h)
	return g
}

func AfterLLMHook(name string, order int, h hooks.Hook[hooks.LLMEvent]) *Group {
	g := NewGroup(name, order)
	g.OnAfterLLM(h)
	return g
}

func ToolLifecycleHook(name string, order int, h hooks.Hook[hooks.ToolEvent]) *Group {
	g := NewGroup(name, order)
	g.OnToolLifecycle(h)
	return g
}

func SessionLifecycleHook(name string, order int, h hooks.Hook[session.LifecycleEvent]) *Group {
	g := NewGroup(name, order)
	g.OnSessionLifecycle(h)
	return g
}

func ErrorHook(name string, order int, h hooks.Hook[hooks.ErrorEvent]) *Group {
	g := NewGroup(name, order)
	g.OnError(h)
	return g
}

// ── Interceptor 便捷构造 ────────────────────────────────────────

func BeforeLLMInterceptor(name string, order int, i hooks.Interceptor[hooks.LLMEvent]) *Group {
	g := NewGroup(name, order)
	g.InterceptBeforeLLM(i)
	return g
}

func AfterLLMInterceptor(name string, order int, i hooks.Interceptor[hooks.LLMEvent]) *Group {
	g := NewGroup(name, order)
	g.InterceptAfterLLM(i)
	return g
}

func ToolLifecycleInterceptor(name string, order int, i hooks.Interceptor[hooks.ToolEvent]) *Group {
	g := NewGroup(name, order)
	g.InterceptToolLifecycle(i)
	return g
}

func SessionLifecycleInterceptor(name string, order int, i hooks.Interceptor[session.LifecycleEvent]) *Group {
	g := NewGroup(name, order)
	g.InterceptSessionLifecycle(i)
	return g
}

func ErrorInterceptor(name string, order int, i hooks.Interceptor[hooks.ErrorEvent]) *Group {
	g := NewGroup(name, order)
	g.InterceptError(i)
	return g
}
