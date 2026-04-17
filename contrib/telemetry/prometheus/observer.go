// Package prometheus provides a observe.Observer implementation that exports
// kernel events as Prometheus metrics.
//
// Usage:
//
//	obs, err := prometheus.New(prom.DefaultRegisterer)
//	if err != nil { ... }
//	kernel.SetObserver(observe.JoinObservers(existing, obs))
package prometheus

import (
	"context"
	"strconv"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	prom "github.com/prometheus/client_golang/prometheus"
)

// Observer implements observe.Observer by recording kernel events as Prometheus metrics.
// It embeds observe.NoOpObserver so that only metrics-relevant methods are overridden.
type Observer struct {
	observe.NoOpObserver

	metrics *observe.MetricsAccumulator

	llmCallsTotal                           *prom.CounterVec
	llmDurationSecs                         *prom.HistogramVec
	llmTokensTotal                          *prom.CounterVec
	llmCostUSDTotal                         *prom.CounterVec
	toolCallsTotal                          *prom.CounterVec
	toolDurationSecs                        *prom.HistogramVec
	contextCompactionsTotal                 *prom.CounterVec
	contextCompactionTokensReclaimedTotal   prom.Counter
	contextTrimRetriesTotal                 prom.Counter
	contextTrimRemovedMessagesTotal         prom.Counter
	contextNormalizationsTotal              prom.Counter
	contextNormalizeDroppedResultsTotal     prom.Counter
	contextNormalizeSynthesizedResultsTotal prom.Counter
	guardianReviewsTotal                    *prom.CounterVec
	sessionsTotal                           *prom.CounterVec
	approvalsTotal                          *prom.CounterVec
	errorsTotal                             *prom.CounterVec
}

// New creates and registers an Observer with the given Prometheus Registerer.
// Pass prom.DefaultRegisterer for process-wide metrics, or a fresh prom.NewRegistry()
// for isolated testing.
func New(reg prom.Registerer) (*Observer, error) {
	o := &Observer{
		metrics: &observe.MetricsAccumulator{},
		llmCallsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_llm_calls_total",
			Help: "Total LLM API calls, labelled by model, stop reason, and whether an error occurred.",
		}, []string{"model", "stop_reason", "error"}),

		llmDurationSecs: prom.NewHistogramVec(prom.HistogramOpts{
			Name:    "moss_llm_call_duration_seconds",
			Help:    "Latency of LLM API calls in seconds.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"model"}),

		llmTokensTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_llm_tokens_total",
			Help: "Tokens consumed in LLM calls (prompt and completion).",
		}, []string{"model", "token_type"}),

		llmCostUSDTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_llm_cost_usd_total",
			Help: "Estimated cumulative LLM cost in USD.",
		}, []string{"model"}),

		toolCallsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_tool_calls_total",
			Help: "Total tool calls, labelled by tool name, risk level, and whether an error occurred.",
		}, []string{"tool_name", "risk", "error"}),

		toolDurationSecs: prom.NewHistogramVec(prom.HistogramOpts{
			Name:    "moss_tool_call_duration_seconds",
			Help:    "Latency of tool calls in seconds.",
			Buckets: prom.DefBuckets,
		}, []string{"tool_name", "risk"}),

		contextCompactionsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_context_compactions_total",
			Help: "Total prompt context compaction events labelled by compaction reason.",
		}, []string{"reason"}),

		contextCompactionTokensReclaimedTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_compaction_tokens_reclaimed_total",
			Help: "Total prompt tokens reclaimed by context compaction.",
		}),

		contextTrimRetriesTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_trim_retries_total",
			Help: "Total prompt trim retries caused by context window pressure.",
		}),

		contextTrimRemovedMessagesTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_trim_removed_messages_total",
			Help: "Total prompt messages removed during trim retries.",
		}),

		contextNormalizationsTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_normalizations_total",
			Help: "Total prompt normalization events.",
		}),

		contextNormalizeDroppedResultsTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_normalize_dropped_tool_results_total",
			Help: "Total orphan tool results dropped during prompt normalization.",
		}),

		contextNormalizeSynthesizedResultsTotal: prom.NewCounter(prom.CounterOpts{
			Name: "moss_context_normalize_synthesized_results_total",
			Help: "Total synthesized tool results inserted during prompt normalization.",
		}),

		guardianReviewsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_guardian_reviews_total",
			Help: "Total guardian review outcomes labelled by outcome.",
		}, []string{"outcome"}),

		sessionsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_sessions_total",
			Help: "Session lifecycle event counts (created/running/completed/failed/cancelled).",
		}, []string{"type"}),

		approvalsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_approvals_total",
			Help: "Approval event counts, labelled by kind and decision outcome.",
		}, []string{"kind", "decision"}),

		errorsTotal: prom.NewCounterVec(prom.CounterOpts{
			Name: "moss_errors_total",
			Help: "Unexpected error counts, labelled by execution phase.",
		}, []string{"phase"}),
	}

	collectors := []prom.Collector{
		o.llmCallsTotal, o.llmDurationSecs, o.llmTokensTotal, o.llmCostUSDTotal,
		o.toolCallsTotal, o.toolDurationSecs,
		o.contextCompactionsTotal, o.contextCompactionTokensReclaimedTotal,
		o.contextTrimRetriesTotal, o.contextTrimRemovedMessagesTotal,
		o.contextNormalizationsTotal, o.contextNormalizeDroppedResultsTotal,
		o.contextNormalizeSynthesizedResultsTotal, o.guardianReviewsTotal,
		o.sessionsTotal, o.approvalsTotal, o.errorsTotal,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
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

func (o *Observer) OnLLMCall(_ context.Context, e observe.LLMCallEvent) {
	o.llmCallsTotal.WithLabelValues(e.Model, e.StopReason, strconv.FormatBool(e.Error != nil)).Inc()
	o.llmDurationSecs.WithLabelValues(e.Model).Observe(e.Duration.Seconds())
	if e.Usage.PromptTokens > 0 {
		o.llmTokensTotal.WithLabelValues(e.Model, "prompt").Add(float64(e.Usage.PromptTokens))
	}
	if e.Usage.CompletionTokens > 0 {
		o.llmTokensTotal.WithLabelValues(e.Model, "completion").Add(float64(e.Usage.CompletionTokens))
	}
	if e.EstimatedCostUSD > 0 {
		o.llmCostUSDTotal.WithLabelValues(e.Model).Add(e.EstimatedCostUSD)
	}
}

func (o *Observer) OnToolCall(_ context.Context, e observe.ToolCallEvent) {
	o.toolCallsTotal.WithLabelValues(e.ToolName, e.Risk, strconv.FormatBool(e.Error != nil)).Inc()
	o.toolDurationSecs.WithLabelValues(e.ToolName, e.Risk).Observe(e.Duration.Seconds())
}

func (o *Observer) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	switch e.Type {
	case observe.ExecutionContextCompacted:
		reason := executionMetricString(e.Metadata, "reason")
		if reason == "" {
			reason = "unknown"
		}
		o.contextCompactionsTotal.WithLabelValues(reason).Inc()
		if reclaimed := executionTokensReclaimed(e.Metadata); reclaimed > 0 {
			o.contextCompactionTokensReclaimedTotal.Add(reclaimed)
		}
	case observe.ExecutionContextTrimRetry:
		o.contextTrimRetriesTotal.Inc()
		if removed := executionMetricFloat(e.Metadata, "messages_removed"); removed > 0 {
			o.contextTrimRemovedMessagesTotal.Add(removed)
		}
	case observe.ExecutionContextNormalized:
		o.contextNormalizationsTotal.Inc()
		if dropped := executionMetricFloat(e.Metadata, "dropped_orphan_tool_results"); dropped > 0 {
			o.contextNormalizeDroppedResultsTotal.Add(dropped)
		}
		if synthesized := executionMetricFloat(e.Metadata, "synthesized_missing_tool_results"); synthesized > 0 {
			o.contextNormalizeSynthesizedResultsTotal.Add(synthesized)
		}
	case observe.ExecutionGuardianReviewed:
		outcome := executionMetricString(e.Metadata, "outcome")
		if outcome == "" {
			outcome = "unknown"
		}
		o.guardianReviewsTotal.WithLabelValues(outcome).Inc()
	}
}

func (o *Observer) OnSessionEvent(_ context.Context, e observe.SessionEvent) {
	o.sessionsTotal.WithLabelValues(e.Type).Inc()
}

func (o *Observer) OnApproval(_ context.Context, e io.ApprovalEvent) {
	o.approvalsTotal.WithLabelValues(string(e.Request.Kind), approvalDecision(e)).Inc()
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

func (o *Observer) OnError(_ context.Context, e observe.ErrorEvent) {
	o.errorsTotal.WithLabelValues(e.Phase).Inc()
}

func executionMetricFloat(meta map[string]any, key string) float64 {
	if len(meta) == 0 {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch value := v.(type) {
	case int:
		return float64(value)
	case int32:
		return float64(value)
	case int64:
		return float64(value)
	case uint:
		return float64(value)
	case uint32:
		return float64(value)
	case uint64:
		return float64(value)
	case float32:
		return float64(value)
	case float64:
		return value
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

func executionTokensReclaimed(meta map[string]any) float64 {
	before := executionMetricFloat(meta, "tokens_before")
	after := executionMetricFloat(meta, "tokens_after")
	if before <= after {
		return 0
	}
	return before - after
}
