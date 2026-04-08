package observe

import (
	"context"
	intr "github.com/mossagents/moss/kernel/interaction"
	mdl "github.com/mossagents/moss/kernel/model"
	"time"
)

// LLMObserver receives LLM call completion events.
type LLMObserver interface {
	OnLLMCall(ctx context.Context, e LLMCallEvent)
}

// ToolObserver receives tool call completion events.
type ToolObserver interface {
	OnToolCall(ctx context.Context, e ToolCallEvent)
}

// ExecutionObserver receives fine-grained execution lifecycle events.
type ExecutionObserver interface {
	OnExecutionEvent(ctx context.Context, e ExecutionEvent)
}

// ApprovalObserver receives approval request and resolution events.
type ApprovalObserver interface {
	OnApproval(ctx context.Context, e intr.ApprovalEvent)
}

// SessionObserver receives session lifecycle events.
type SessionObserver interface {
	OnSessionEvent(ctx context.Context, e SessionEvent)
}

// ErrorObserver receives unexpected error events.
type ErrorObserver interface {
	OnError(ctx context.Context, e ErrorEvent)
}

// EventObserver 接收统一事件封装。
type EventObserver interface {
	OnEvent(ctx context.Context, e EventEnvelope)
}

// Observer 是 Kernel 运行事件的观察者接口。
// 上层应用实现此接口对接 OpenTelemetry / Prometheus / slog 等。
// 默认使用 NoOpObserver（零开销）。
//
// 若只需观察部分事件，可实现对应的细粒度子接口（LLMObserver、
// ToolObserver、ExecutionObserver 等），然后嵌入 NoOpObserver
// 补全其余方法，以减少无关 stub 代码。
type Observer interface {
	LLMObserver
	ToolObserver
	ExecutionObserver
	ApprovalObserver
	SessionObserver
	ErrorObserver
}

// JoinObservers 组合多个 Observer，按传入顺序依次分发事件。
// nil Observer 会被忽略；当没有有效 Observer 时返回 NoOpObserver。
func JoinObservers(observers ...Observer) Observer {
	filtered := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if observer == nil {
			continue
		}
		filtered = append(filtered, observer)
	}
	switch len(filtered) {
	case 0:
		return NoOpObserver{}
	case 1:
		return filtered[0]
	default:
		return joinedObserver(filtered)
	}
}

// LLMCallEvent 记录一次 LLM 调用的指标。
type LLMCallEvent struct {
	SessionID        string         `json:"session_id"`
	Model            string         `json:"model,omitempty"`
	StartedAt        time.Time      `json:"started_at,omitempty"`
	Duration         time.Duration  `json:"duration_ms"`
	Usage            mdl.TokenUsage `json:"usage"`
	EstimatedCostUSD float64        `json:"estimated_cost_usd,omitempty"`
	StopReason       string         `json:"stop_reason,omitempty"`
	Streamed         bool           `json:"streamed"`
	Error            error          `json:"-"`
}

// ToolCallEvent 记录一次工具调用的指标。
type ToolCallEvent struct {
	SessionID string        `json:"session_id"`
	ToolName  string        `json:"tool_name"`
	Risk      string        `json:"risk,omitempty"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	Duration  time.Duration `json:"duration_ms"`
	Error     error         `json:"-"`
}

// SessionEvent 记录 Session 生命周期事件。
type SessionEvent struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"` // "created", "running", "completed", "failed", "cancelled"
}

// ErrorEvent 记录非预期错误。
type ErrorEvent struct {
	SessionID string `json:"session_id,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Error     error  `json:"-"`
	Message   string `json:"message"`
}

// NoOpObserver 是不做任何操作的默认 Observer 实现。
type NoOpObserver struct{}

func (NoOpObserver) OnLLMCall(_ context.Context, _ LLMCallEvent)          {}
func (NoOpObserver) OnToolCall(_ context.Context, _ ToolCallEvent)        {}
func (NoOpObserver) OnExecutionEvent(_ context.Context, _ ExecutionEvent) {}
func (NoOpObserver) OnApproval(_ context.Context, _ intr.ApprovalEvent)   {}
func (NoOpObserver) OnSessionEvent(_ context.Context, _ SessionEvent)     {}
func (NoOpObserver) OnError(_ context.Context, _ ErrorEvent)              {}
func (NoOpObserver) OnEvent(_ context.Context, _ EventEnvelope)           {}

type joinedObserver []Observer

func (o joinedObserver) OnLLMCall(ctx context.Context, e LLMCallEvent) {
	for _, observer := range o {
		observer.OnLLMCall(ctx, e)
	}
}

func (o joinedObserver) OnToolCall(ctx context.Context, e ToolCallEvent) {
	for _, observer := range o {
		observer.OnToolCall(ctx, e)
	}
}

func (o joinedObserver) OnExecutionEvent(ctx context.Context, e ExecutionEvent) {
	for _, observer := range o {
		observer.OnExecutionEvent(ctx, e)
	}
}

func (o joinedObserver) OnApproval(ctx context.Context, e intr.ApprovalEvent) {
	for _, observer := range o {
		observer.OnApproval(ctx, e)
	}
}

func (o joinedObserver) OnSessionEvent(ctx context.Context, e SessionEvent) {
	for _, observer := range o {
		observer.OnSessionEvent(ctx, e)
	}
}

func (o joinedObserver) OnError(ctx context.Context, e ErrorEvent) {
	for _, observer := range o {
		observer.OnError(ctx, e)
	}
}

func (o joinedObserver) OnEvent(ctx context.Context, e EventEnvelope) {
	for _, observer := range o {
		if aware, ok := observer.(EventObserver); ok {
			aware.OnEvent(ctx, e)
		}
	}
}

func ObserveLLMCall(ctx context.Context, observer LLMObserver, e LLMCallEvent) {
	if observer == nil {
		return
	}
	observer.OnLLMCall(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromLLMCall(e))
}

func ObserveToolCall(ctx context.Context, observer ToolObserver, e ToolCallEvent) {
	if observer == nil {
		return
	}
	observer.OnToolCall(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromToolCall(e))
}

func ObserveExecutionEvent(ctx context.Context, observer ExecutionObserver, e ExecutionEvent) {
	if observer == nil {
		return
	}
	observer.OnExecutionEvent(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromExecutionEvent(e))
}

func ObserveApproval(ctx context.Context, observer ApprovalObserver, e intr.ApprovalEvent) {
	if observer == nil {
		return
	}
	observer.OnApproval(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromApprovalEvent(e))
}

func ObserveSessionEvent(ctx context.Context, observer SessionObserver, e SessionEvent) {
	if observer == nil {
		return
	}
	observer.OnSessionEvent(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromSessionEvent(e))
}

func ObserveError(ctx context.Context, observer ErrorObserver, e ErrorEvent) {
	if observer == nil {
		return
	}
	observer.OnError(ctx, e)
	observeEvent(ctx, observer, EnvelopeFromErrorEvent(e))
}

func ObserveTaskEvent(ctx context.Context, observer any, e TaskEvent) {
	observeEvent(ctx, observer, EnvelopeFromTaskEvent(e))
}

func observeEvent(ctx context.Context, observer any, e EventEnvelope) {
	if aware, ok := observer.(EventObserver); ok {
		aware.OnEvent(ctx, e)
	}
}
