package product

import (
	"context"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

type TraceEvent struct {
	Kind             string         `json:"kind"`
	Timestamp        time.Time      `json:"timestamp"`
	SessionID        string         `json:"session_id,omitempty"`
	Type             string         `json:"type,omitempty"`
	Model            string         `json:"model,omitempty"`
	ToolName         string         `json:"tool_name,omitempty"`
	DurationMS       int64          `json:"duration_ms,omitempty"`
	PromptTokens     int            `json:"prompt_tokens,omitempty"`
	CompletionTokens int            `json:"completion_tokens,omitempty"`
	TotalTokens      int            `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64        `json:"estimated_cost_usd,omitempty"`
	Error            string         `json:"error,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
}

type RunTrace struct {
	PromptTokens     int          `json:"prompt_tokens,omitempty"`
	CompletionTokens int          `json:"completion_tokens,omitempty"`
	TotalTokens      int          `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64      `json:"estimated_cost_usd,omitempty"`
	LLMCalls         int          `json:"llm_calls,omitempty"`
	ToolCalls        int          `json:"tool_calls,omitempty"`
	Timeline         []TraceEvent `json:"timeline,omitempty"`
}

type RunTraceRecorder struct {
	mu    sync.Mutex
	trace RunTrace
}

func NewRunTraceRecorder() *RunTraceRecorder {
	return &RunTraceRecorder{}
}

func (r *RunTraceRecorder) Snapshot() RunTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.trace
	out.Timeline = append([]TraceEvent(nil), r.trace.Timeline...)
	return out
}

func (r *RunTraceRecorder) OnLLMCall(_ context.Context, e port.LLMCallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.LLMCalls++
	r.trace.PromptTokens += e.Usage.PromptTokens
	r.trace.CompletionTokens += e.Usage.CompletionTokens
	r.trace.TotalTokens += e.Usage.TotalTokens
	r.trace.EstimatedCostUSD += e.EstimatedCostUSD
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:             "llm_call",
		Timestamp:        time.Now().UTC(),
		SessionID:        e.SessionID,
		Model:            e.Model,
		DurationMS:       e.Duration.Milliseconds(),
		PromptTokens:     e.Usage.PromptTokens,
		CompletionTokens: e.Usage.CompletionTokens,
		TotalTokens:      e.Usage.TotalTokens,
		EstimatedCostUSD: e.EstimatedCostUSD,
		Type:             e.StopReason,
		Error:            errorString(e.Error),
	})
}

func (r *RunTraceRecorder) OnToolCall(_ context.Context, e port.ToolCallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.ToolCalls++
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:       "tool_call",
		Timestamp:  time.Now().UTC(),
		SessionID:  e.SessionID,
		ToolName:   e.ToolName,
		DurationMS: e.Duration.Milliseconds(),
		Error:      errorString(e.Error),
	})
}

func (r *RunTraceRecorder) OnExecutionEvent(_ context.Context, e port.ExecutionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:       "execution_event",
		Timestamp:  e.Timestamp.UTC(),
		SessionID:  e.SessionID,
		Type:       string(e.Type),
		Model:      e.Model,
		ToolName:   e.ToolName,
		DurationMS: e.Duration.Milliseconds(),
		Error:      e.Error,
		Data:       cloneTraceData(e.Data),
	})
}

func (r *RunTraceRecorder) OnApproval(_ context.Context, e port.ApprovalEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "approval",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Type,
		ToolName:  e.Request.ToolName,
		Data: map[string]any{
			"id":          e.Request.ID,
			"reason":      e.Request.Reason,
			"reason_code": e.Request.ReasonCode,
		},
	})
}

func (r *RunTraceRecorder) OnSessionEvent(_ context.Context, e port.SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "session",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Type,
	})
}

func (r *RunTraceRecorder) OnError(_ context.Context, e port.ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "error",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Phase,
		Error:     e.Message,
	})
}

type pricingObserver struct {
	catalog *PricingCatalog
	next    port.Observer
}

func NewPricingObserver(catalog *PricingCatalog, next port.Observer) port.Observer {
	if next == nil {
		return nil
	}
	if catalog == nil {
		return next
	}
	return pricingObserver{catalog: catalog, next: next}
}

func (o pricingObserver) OnLLMCall(ctx context.Context, e port.LLMCallEvent) {
	if cost, ok := o.catalog.Estimate(e.Usage, e.Model); ok {
		e.EstimatedCostUSD = cost
	}
	o.next.OnLLMCall(ctx, e)
}

func (o pricingObserver) OnToolCall(ctx context.Context, e port.ToolCallEvent) {
	o.next.OnToolCall(ctx, e)
}

func (o pricingObserver) OnExecutionEvent(ctx context.Context, e port.ExecutionEvent) {
	o.next.OnExecutionEvent(ctx, e)
}

func (o pricingObserver) OnApproval(ctx context.Context, e port.ApprovalEvent) {
	o.next.OnApproval(ctx, e)
}

func (o pricingObserver) OnSessionEvent(ctx context.Context, e port.SessionEvent) {
	o.next.OnSessionEvent(ctx, e)
}

func (o pricingObserver) OnError(ctx context.Context, e port.ErrorEvent) {
	o.next.OnError(ctx, e)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cloneTraceData(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
