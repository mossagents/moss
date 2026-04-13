package hooks

import "github.com/mossagents/moss/kernel/session"

// Registry 管理 Agent 运行时所有生命周期阶段的 hook pipeline。
//
// Registry 处理 Agent Loop 运行中的钩子（LLM 调用前后、工具调用前后、会话/工具生命周期、错误）。
// Kernel 级的启动/关停、System Prompt 组装与状态槽分别由 StageRegistry、
// PromptAssembler 与 ServiceRegistry 负责。
type Registry struct {
	BeforeLLM          *Pipeline[LLMEvent]
	AfterLLM           *Pipeline[LLMEvent]
	BeforeToolCall     *Pipeline[ToolEvent]
	AfterToolCall      *Pipeline[ToolEvent]
	OnSessionStart     *Pipeline[SessionEvent]
	OnSessionEnd       *Pipeline[SessionEvent]
	OnSessionLifecycle *Pipeline[session.LifecycleEvent]
	OnToolLifecycle    *Pipeline[session.ToolLifecycleEvent]
	OnError            *Pipeline[ErrorEvent]
}

// NewRegistry 创建包含所有 pipeline 的空 Registry。
func NewRegistry() *Registry {
	return &Registry{
		BeforeLLM:          NewPipeline[LLMEvent](),
		AfterLLM:           NewPipeline[LLMEvent](),
		BeforeToolCall:     NewPipeline[ToolEvent](),
		AfterToolCall:      NewPipeline[ToolEvent](),
		OnSessionStart:     NewPipeline[SessionEvent](),
		OnSessionEnd:       NewPipeline[SessionEvent](),
		OnSessionLifecycle: NewPipeline[session.LifecycleEvent](),
		OnToolLifecycle:    NewPipeline[session.ToolLifecycleEvent](),
		OnError:            NewPipeline[ErrorEvent](),
	}
}
