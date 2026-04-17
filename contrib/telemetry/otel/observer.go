// Package otel provides a observe.Observer implementation that records kernel
// events as OpenTelemetry metrics using the standard OTEL Metrics API.
//
// Usage:
//
//	mp := // your MeterProvider (SDK, OTLP exporter, etc.)
//	obs, err := otel.New(mp.Meter("github.com/mossagents/moss"))
//	if err != nil { ... }
//	kernel.SetObserver(observe.JoinObservers(existing, obs))
package otel

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Observer implements observe.Observer by recording kernel events as OTEL metrics.
// It embeds observe.NoOpObserver so only metrics-relevant methods are overridden.
type Observer struct {
	observe.NoOpObserver

	metrics *observe.MetricsAccumulator

	llmCallsTotal  metric.Int64Counter
	llmDurationMs  metric.Int64Histogram
	llmTokensTotal metric.Int64Counter
	llmCostUSD     metric.Float64Counter

	toolCallsTotal                      metric.Int64Counter
	toolDurationMs                      metric.Int64Histogram
	contextCompactionsTotal             metric.Int64Counter
	contextCompactionTokensReclaimed    metric.Int64Counter
	contextTrimRetriesTotal             metric.Int64Counter
	contextTrimRemovedMessagesTotal     metric.Int64Counter
	contextNormalizationsTotal          metric.Int64Counter
	contextNormalizeDroppedResultsTotal metric.Int64Counter
	contextNormalizeSynthResultsTotal   metric.Int64Counter
	guardianReviewsTotal                metric.Int64Counter

	sessionsTotal  metric.Int64Counter
	approvalsTotal metric.Int64Counter
	errorsTotal    metric.Int64Counter
}

// New creates an Observer using instruments from the given OTEL Meter.
// All instruments use the "moss." namespace prefix and standard OTEL conventions.
func New(meter metric.Meter) (*Observer, error) {
	o := &Observer{metrics: &observe.MetricsAccumulator{}}
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

	if o.contextCompactionsTotal, err = meter.Int64Counter("moss.context.compactions",
		metric.WithDescription("Total prompt context compaction events."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.compactions: %w", err)
	}

	if o.contextCompactionTokensReclaimed, err = meter.Int64Counter("moss.context.compaction_tokens_reclaimed",
		metric.WithDescription("Total prompt tokens reclaimed by context compaction."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.compaction_tokens_reclaimed: %w", err)
	}

	if o.contextTrimRetriesTotal, err = meter.Int64Counter("moss.context.trim_retries",
		metric.WithDescription("Total prompt trim retries caused by context window pressure."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.trim_retries: %w", err)
	}

	if o.contextTrimRemovedMessagesTotal, err = meter.Int64Counter("moss.context.trim_removed_messages",
		metric.WithDescription("Total prompt messages removed during trim retries."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.trim_removed_messages: %w", err)
	}

	if o.contextNormalizationsTotal, err = meter.Int64Counter("moss.context.normalizations",
		metric.WithDescription("Total prompt normalization events."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.normalizations: %w", err)
	}

	if o.contextNormalizeDroppedResultsTotal, err = meter.Int64Counter("moss.context.normalize_dropped_tool_results",
		metric.WithDescription("Total orphan tool results dropped during prompt normalization."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.normalize_dropped_tool_results: %w", err)
	}

	if o.contextNormalizeSynthResultsTotal, err = meter.Int64Counter("moss.context.normalize_synthesized_results",
		metric.WithDescription("Total synthesized tool results inserted during prompt normalization."),
	); err != nil {
		return nil, fmt.Errorf("moss.context.normalize_synthesized_results: %w", err)
	}

	if o.guardianReviewsTotal, err = meter.Int64Counter("moss.guardian.reviews",
		metric.WithDescription("Total guardian review outcomes."),
	); err != nil {
		return nil, fmt.Errorf("moss.guardian.reviews: %w", err)
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

func (o *Observer) OnEvent(_ context.Context, e observe.EventEnvelope) {
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

func (o *Observer) OnLLMCall(ctx context.Context, e observe.LLMCallEvent) {
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

func (o *Observer) OnToolCall(ctx context.Context, e observe.ToolCallEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("tool_name", e.ToolName),
		attribute.String("risk", e.Risk),
		attribute.Bool("error", e.Error != nil),
	}
	o.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	o.toolDurationMs.Record(ctx, e.Duration.Milliseconds(),
		metric.WithAttributes(attribute.String("tool_name", e.ToolName), attribute.String("risk", e.Risk)))
}

func (o *Observer) OnExecutionEvent(ctx context.Context, e observe.ExecutionEvent) {
	switch e.Type {
	case observe.ExecutionContextCompacted:
		reason := executionMetricString(e.Metadata, "reason")
		if reason == "" {
			reason = "unknown"
		}
		o.contextCompactionsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
		reclaimed := executionTokensReclaimed(e.Metadata)
		if reclaimed > 0 {
			o.contextCompactionTokensReclaimed.Add(ctx, reclaimed)
		}
	case observe.ExecutionContextTrimRetry:
		o.contextTrimRetriesTotal.Add(ctx, 1)
		if removed := executionMetricInt64(e.Metadata, "messages_removed"); removed > 0 {
			o.contextTrimRemovedMessagesTotal.Add(ctx, removed)
		}
	case observe.ExecutionContextNormalized:
		o.contextNormalizationsTotal.Add(ctx, 1)
		if dropped := executionMetricInt64(e.Metadata, "dropped_orphan_tool_results"); dropped > 0 {
			o.contextNormalizeDroppedResultsTotal.Add(ctx, dropped)
		}
		if synthesized := executionMetricInt64(e.Metadata, "synthesized_missing_tool_results"); synthesized > 0 {
			o.contextNormalizeSynthResultsTotal.Add(ctx, synthesized)
		}
	case observe.ExecutionGuardianReviewed:
		outcome := executionMetricString(e.Metadata, "outcome")
		if outcome == "" {
			outcome = "unknown"
		}
		o.guardianReviewsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func (o *Observer) OnSessionEvent(ctx context.Context, e observe.SessionEvent) {
	o.sessionsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("type", e.Type)))
}

func (o *Observer) OnApproval(ctx context.Context, e io.ApprovalEvent) {
	o.approvalsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", string(e.Request.Kind)),
		attribute.String("decision", approvalDecision(e)),
	))
}

func approvalDecision(e io.ApprovalEvent) string {
	if e.Decision == nil {
		return "pending"
	}
	if e.Decision.Approved {
		return "approved"
	}
	return "rejected"
}

func (o *Observer) OnError(ctx context.Context, e observe.ErrorEvent) {
	o.errorsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("phase", e.Phase)))
}

func executionMetricInt64(meta map[string]any, key string) int64 {
	if len(meta) == 0 {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch value := v.(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case uint:
		return int64(value)
	case uint32:
		return int64(value)
	case uint64:
		return int64(value)
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func executionMetricString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return ""
	}
	if value, ok := v.(string); ok {
		return value
	}
	return ""
}

func executionTokensReclaimed(meta map[string]any) int64 {
	before := executionMetricInt64(meta, "tokens_before")
	after := executionMetricInt64(meta, "tokens_after")
	if before <= after {
		return 0
	}
	return before - after
}
