package hooks

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/mossagents/moss/kernel/session"
)

// Registry 管理 Agent 运行时所有生命周期阶段的 hook pipeline。
//
// Registry 处理 Agent Loop 运行中的钩子（LLM 调用前后、工具/会话生命周期、错误）。
// Kernel 级的启动/关停、System Prompt 组装与状态槽分别由 StageRegistry、
// PromptAssembler 与 ServiceRegistry 负责。
type Registry struct {
	BeforeLLM          *Pipeline[LLMEvent]
	AfterLLM           *Pipeline[LLMEvent]
	OnSessionLifecycle *Pipeline[session.LifecycleEvent]
	OnToolLifecycle    *Pipeline[ToolEvent]
	OnError            *Pipeline[ErrorEvent]

	// toolPolicyGate 是在 OnToolLifecycle pipeline 之前执行的不可绕过的权限门控。
	// 与 OnToolLifecycle 中的 hook 不同，拦截器无法绕过此门控。
	// 仅在 ToolLifecycleBefore 阶段执行。
	toolPolicyMu   sync.RWMutex
	toolPolicyGate func(context.Context, *ToolEvent) error

	// trusted 控制 BeforeLLM、AfterLLM 和 OnToolLifecycle hook 是否执行。
	// 为 false 时仅 toolPolicyGate 继续执行。默认为 true。
	trusted atomic.Bool
}

// NewRegistry 创建包含所有 pipeline 的空 Registry。
func NewRegistry() *Registry {
	r := &Registry{
		BeforeLLM:          NewPipeline[LLMEvent](),
		AfterLLM:           NewPipeline[LLMEvent](),
		OnSessionLifecycle: NewPipeline[session.LifecycleEvent](),
		OnToolLifecycle:    NewPipeline[ToolEvent](),
		OnError:            NewPipeline[ErrorEvent](),
	}
	r.trusted.Store(true)
	return r
}

// SetToolPolicyGate 设置不可绕过的权限门控函数。
// 门控在 ToolLifecycleBefore 阶段运行，先于任何 OnToolLifecycle hook。
// 拦截器无法绕过此门控，因为它在 pipeline 之外直接调用。
func (r *Registry) SetToolPolicyGate(fn func(context.Context, *ToolEvent) error) {
	r.toolPolicyMu.Lock()
	r.toolPolicyGate = fn
	r.toolPolicyMu.Unlock()
}

// RunToolPolicyGate 执行已注册的权限门控（如果有）。
// 仅在 ToolLifecycleBefore 阶段调用。
func (r *Registry) RunToolPolicyGate(ctx context.Context, ev *ToolEvent) error {
	if r == nil || ev == nil {
		return nil
	}
	if ev.Stage != ToolLifecycleBefore {
		return nil
	}
	r.toolPolicyMu.RLock()
	gate := r.toolPolicyGate
	r.toolPolicyMu.RUnlock()
	if gate == nil {
		return nil
	}
	return gate(ctx, ev)
}

// SetTrusted 设置 hook 系统的信任状态。
// 为 false 时，BeforeLLM、AfterLLM 和 OnToolLifecycle pipeline 将跳过执行，
// 但 toolPolicyGate 仍然执行。
func (r *Registry) SetTrusted(trusted bool) {
	if r != nil {
		r.trusted.Store(trusted)
	}
}

// IsTrusted 返回 hook 系统是否处于信任状态。
func (r *Registry) IsTrusted() bool {
	return r != nil && r.trusted.Load()
}
