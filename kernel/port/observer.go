package port

import (
	"context"
	"time"
)

// Observer 是 Kernel 运行事件的观察者接口。
// 上层应用实现此接口对接 OpenTelemetry / Prometheus / slog 等。
// 默认使用 NoOpObserver（零开销）。
type Observer interface {
	// OnLLMCall 在 LLM 调用完成后触发（无论成功或失败）。
	OnLLMCall(ctx context.Context, e LLMCallEvent)
	// OnToolCall 在工具调用完成后触发（无论成功或失败）。
	OnToolCall(ctx context.Context, e ToolCallEvent)
	// OnExecutionEvent 在统一执行事件产生时触发。
	OnExecutionEvent(ctx context.Context, e ExecutionEvent)
	// OnApproval 在审批请求发起和完成时触发。
	OnApproval(ctx context.Context, e ApprovalEvent)
	// OnSessionEvent 在 Session 生命周期事件时触发。
	OnSessionEvent(ctx context.Context, e SessionEvent)
	// OnError 在非预期错误发生时触发。
	OnError(ctx context.Context, e ErrorEvent)
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
	SessionID  string        `json:"session_id"`
	Model      string        `json:"model,omitempty"`
	Duration   time.Duration `json:"duration_ms"`
	Usage      TokenUsage    `json:"usage"`
	StopReason string        `json:"stop_reason,omitempty"`
	Streamed   bool          `json:"streamed"`
	Error      error         `json:"-"`
}

// ToolCallEvent 记录一次工具调用的指标。
type ToolCallEvent struct {
	SessionID string        `json:"session_id"`
	ToolName  string        `json:"tool_name"`
	Risk      string        `json:"risk,omitempty"`
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
func (NoOpObserver) OnApproval(_ context.Context, _ ApprovalEvent)        {}
func (NoOpObserver) OnSessionEvent(_ context.Context, _ SessionEvent)     {}
func (NoOpObserver) OnError(_ context.Context, _ ErrorEvent)              {}

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

func (o joinedObserver) OnApproval(ctx context.Context, e ApprovalEvent) {
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
