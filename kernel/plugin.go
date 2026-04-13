package kernel

import (
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// Plugin 将相关的生命周期 hook 组织为一个命名单元。
// 只设置需要的字段，nil 字段会被忽略。
//
// 对于需要拦截器（Interceptor / 洋葱模式）的场景，请使用
// WithPluginInstaller 或 Kernel.InstallHooks 直接操作 hooks.Registry。
type Plugin struct {
	Name  string // 插件名称，用于日志和调试
	Order int    // 执行优先级（值越小越先执行，默认 0）

	BeforeLLM          hooks.Hook[hooks.LLMEvent]
	AfterLLM           hooks.Hook[hooks.LLMEvent]
	BeforeToolCall     hooks.Hook[hooks.ToolEvent]
	AfterToolCall      hooks.Hook[hooks.ToolEvent]
	OnSessionStart     hooks.Hook[hooks.SessionEvent]
	OnSessionEnd       hooks.Hook[hooks.SessionEvent]
	OnSessionLifecycle hooks.Hook[session.LifecycleEvent]
	OnToolLifecycle    hooks.Hook[session.ToolLifecycleEvent]
	OnError            hooks.Hook[hooks.ErrorEvent]
}

// installPlugin 将 Plugin 中所有非 nil 的 hook 注册到 Registry。
func installPlugin(reg *hooks.Registry, p Plugin) {
	if p.BeforeLLM != nil {
		reg.BeforeLLM.AddHook(p.Name, p.BeforeLLM, p.Order)
	}
	if p.AfterLLM != nil {
		reg.AfterLLM.AddHook(p.Name, p.AfterLLM, p.Order)
	}
	if p.BeforeToolCall != nil {
		reg.BeforeToolCall.AddHook(p.Name, p.BeforeToolCall, p.Order)
	}
	if p.AfterToolCall != nil {
		reg.AfterToolCall.AddHook(p.Name, p.AfterToolCall, p.Order)
	}
	if p.OnSessionStart != nil {
		reg.OnSessionStart.AddHook(p.Name, p.OnSessionStart, p.Order)
	}
	if p.OnSessionEnd != nil {
		reg.OnSessionEnd.AddHook(p.Name, p.OnSessionEnd, p.Order)
	}
	if p.OnSessionLifecycle != nil {
		reg.OnSessionLifecycle.AddHook(p.Name, p.OnSessionLifecycle, p.Order)
	}
	if p.OnToolLifecycle != nil {
		reg.OnToolLifecycle.AddHook(p.Name, p.OnToolLifecycle, p.Order)
	}
	if p.OnError != nil {
		reg.OnError.AddHook(p.Name, p.OnError, p.Order)
	}
}
