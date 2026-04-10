package hooks

// Registry 管理 Agent 运行时所有生命周期阶段的 hook pipeline。
//
// Registry 处理 Agent Loop 运行中的钩子（LLM 调用前后、工具调用前后、会话开始/结束、错误）。
// 与 ExtensionBridge 的区别：ExtensionBridge 处理 Kernel 级生命周期（启动、关停、System Prompt 组装），
// 两者作用域互补，不重叠。
type Registry struct {
	BeforeLLM      *Pipeline[LLMEvent]
	AfterLLM       *Pipeline[LLMEvent]
	BeforeToolCall *Pipeline[ToolEvent]
	AfterToolCall  *Pipeline[ToolEvent]
	OnSessionStart *Pipeline[SessionEvent]
	OnSessionEnd   *Pipeline[SessionEvent]
	OnError        *Pipeline[ErrorEvent]
}

// NewRegistry 创建包含所有 pipeline 的空 Registry。
func NewRegistry() *Registry {
	return &Registry{
		BeforeLLM:      NewPipeline[LLMEvent](),
		AfterLLM:       NewPipeline[LLMEvent](),
		BeforeToolCall: NewPipeline[ToolEvent](),
		AfterToolCall:  NewPipeline[ToolEvent](),
		OnSessionStart: NewPipeline[SessionEvent](),
		OnSessionEnd:   NewPipeline[SessionEvent](),
		OnError:        NewPipeline[ErrorEvent](),
	}
}
