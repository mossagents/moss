package hooks

// Registry 管理 Agent 运行时所有生命周期阶段的 hook pipeline。
// 它统一替代了 middleware.Chain 和 ExtensionBridge 的运行时 hook。
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
