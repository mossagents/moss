// Package otel provides a kobs.Observer implementation that records kernel
// events as OpenTelemetry metrics using the standard OTEL Metrics API.
//
// Usage:
//
//	mp := // your MeterProvider (SDK, OTLP exporter, etc.)
//	obs, err := otel.New(mp.Meter("github.com/mossagents/moss"))
//	if err != nil { ... }
//	kernel.SetObserver(kobs.JoinObservers(existing, obs))
package otel

import (
	"context"
	"fmt"

	intr "github.com/mossagents/moss/kernel/io"
	kobs "github.com/mossagents/moss/kernel/observe"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Observer implements kobs.Observer by recording kernel events as OTEL metrics.
// It embeds kobs.NoOpObserver so only metrics-relevant methods are overridden;
// fine-grained execution events (OnExecutionEvent) are silently discarded.
type Observer struct {
	kobs.NoOpObserver

	metrics *kobs.MetricsAccumulator

	llmCallsTotal  metric.Int64Counter
	llmDurationMs  metric.Int64Histogram
	llmTokensTotal metric.Int64Counter
	llmCostUSD     metric.Float64Counter

	toolCallsTotal metric.Int64Counter
	toolDurationMs metric.Int64Histogram

	sessionsTotal  metric.Int64Counter
	approvalsTotal metric.Int64Counter
	errorsTotal    metric.Int64Counter
}

// New creates an Observer using instruments from the given OTEL Meter.
// All instruments use the "moss." namespace prefix and standard OTEL conventions.
func New(meter metric.Meter) (*Observer, error) {
	o := &Observer{metrics: &kobs.MetricsAccumulator{}}
	var err error

	if o.llmCallsTotal, err = meter.Int64Counter("moss.llm.calls",
		metric.WithDescription("Total LLM API calls."),
	); err != nil {
		return nil, fmt.Errorf("moss.llm.calls: %w", err)
	}

	if o.llmDurationMs, err = meter.Int64Histogram("moss.llm.duration",
		metric.WithDescription("LLM call latency in milliseconds."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("moss.llm.duration: %w", err)
	}

	if o.llmTokensTotal, err = meter.Int64Counter("moss.llm.tokens",
		metric.WithDescription("Tokens consumed in LLM calls (prompt and completion)."),
	); err != nil {
		return nil, fmt.Errorf("moss.llm.tokens: %w", err)
	}

	if o.llmCostUSD, err = meter.Float64Counter("moss.llm.cost_usd",
		metric.WithDescription("Estimated cumulative LLM cost in USD."),
		metric.WithUnit("USD"),
	); err != nil {
		return nil, fmt.Errorf("moss.llm.cost_usd: %w", err)
	}

	if o.toolCallsTotal, err = meter.Int64Counter("moss.tool.calls",
		metric.WithDescription("Total tool calls."),
	); err != nil {
		return nil, fmt.Errorf("moss.tool.calls: %w", err)
	}

	if o.toolDurationMs, err = meter.Int64Histogram("moss.tool.duration",
		metric.WithDescription("Tool call latency in milliseconds."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("moss.tool.duration: %w", err)
	}

	if o.sessionsTotal, err = meter.Int64Counter("moss.sessions",
		metric.WithDescription("Session lifecycle event counts."),
	); err != nil {
		return nil, fmt.Errorf("moss.sessions: %w", err)
	}

	if o.approvalsTotal, err = meter.Int64Counter("moss.approvals",
		metric.WithDescription("Approval event counts."),
	); err != nil {
		return nil, fmt.Errorf("moss.approvals: %w", err)
	}

	if o.errorsTotal, err = meter.Int64Counter("moss.errors",
		metric.WithDescription("Unexpected error counts."),
	); err != nil {
		return nil, fmt.Errorf("moss.errors: %w", err)
	}

	return o, nil
}

func (o *Observer) OnEvent(_ context.Context, e kobs.EventEnvelope) {
	if o.metrics != nil {
		o.metrics.ApplyEnvelope(e)
	}
}

// NormalizedMetricsMap returns unified success/latency/cost/tool-error counters.
func (o *Observer) NormalizedMetricsMap() map[string]float64 {
	if o.metrics == nil {
		return map[string]float64{}
	}
	return o.metrics.Map()
}

func (o *Observer) OnLLMCall(ctx context.Context, e kobs.LLMCallEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("model", e.Model),
		attribute.String("stop_reason", e.StopReason),
		attribute.Bool("error", e.Error != nil),
	}
	o.llmCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	o.llmDurationMs.Record(ctx, e.Duration.Milliseconds(),
		metric.WithAttributes(attribute.String("model", e.Model)))
	if e.Usage.PromptTokens > 0 {
		o.llmTokensTotal.Add(ctx, int64(e.Usage.PromptTokens),
			metric.WithAttributes(attribute.String("model", e.Model), attribute.String("token_type", "prompt")))
	}
	if e.Usage.CompletionTokens > 0 {
		o.llmTokensTotal.Add(ctx, int64(e.Usage.CompletionTokens),
			metric.WithAttributes(attribute.String("model", e.Model), attribute.String("token_type", "completion")))
	}
	if e.EstimatedCostUSD > 0 {
		o.llmCostUSD.Add(ctx, e.EstimatedCostUSD,
			metric.WithAttributes(attribute.String("model", e.Model)))
	}
}

func (o *Observer) OnToolCall(ctx context.Context, e kobs.ToolCallEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("tool_name", e.ToolName),
		attribute.String("risk", e.Risk),
		attribute.Bool("error", e.Error != nil),
	}
	o.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	o.toolDurationMs.Record(ctx, e.Duration.Milliseconds(),
		metric.WithAttributes(attribute.String("tool_name", e.ToolName), attribute.String("risk", e.Risk)))
}

func (o *Observer) OnSessionEvent(ctx context.Context, e kobs.SessionEvent) {
	o.sessionsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("type", e.Type)))
}

func (o *Observer) OnApproval(ctx context.Context, e intr.ApprovalEvent) {
	o.approvalsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", string(e.Request.Kind)),
		attribute.String("decision", approvalDecision(e)),
	))
}

func approvalDecision(e intr.ApprovalEvent) string {
	if e.Decision == nil {
		return "pending"
	}
	if e.Decision.Approved {
		return "approved"
	}
	return "rejected"
}

func (o *Observer) OnError(ctx context.Context, e kobs.ErrorEvent) {
	o.errorsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("phase", e.Phase)))
}
