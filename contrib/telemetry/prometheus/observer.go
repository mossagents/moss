// Package prometheus provides a kobs.Observer implementation that exports
// kernel events as Prometheus metrics.
//
// Usage:
//
//	obs, err := prometheus.New(prom.DefaultRegisterer)
//	if err != nil { ... }
//	kernel.SetObserver(kobs.JoinObservers(existing, obs))
package prometheus

import (
	"context"
	intr "github.com/mossagents/moss/kernel/io"
	kobs "github.com/mossagents/moss/kernel/observe"
	prom "github.com/prometheus/client_golang/prometheus"
	"strconv"
)

// Observer implements kobs.Observer by recording kernel events as Prometheus metrics.
// It embeds kobs.NoOpObserver so that only metrics-relevant methods are overridden;
// fine-grained execution events (OnExecutionEvent) are silently discarded.
type Observer struct {
	kobs.NoOpObserver

	llmCallsTotal    *prom.CounterVec
	llmDurationSecs  *prom.HistogramVec
	llmTokensTotal   *prom.CounterVec
	llmCostUSDTotal  *prom.CounterVec
	toolCallsTotal   *prom.CounterVec
	toolDurationSecs *prom.HistogramVec
	sessionsTotal    *prom.CounterVec
	approvalsTotal   *prom.CounterVec
	errorsTotal      *prom.CounterVec
}

// New creates and registers an Observer with the given Prometheus Registerer.
// Pass prom.DefaultRegisterer for process-wide metrics, or a fresh prom.NewRegistry()
// for isolated testing.
func New(reg prom.Registerer) (*Observer, error) {
	o := &Observer{
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
		o.sessionsTotal, o.approvalsTotal, o.errorsTotal,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (o *Observer) OnLLMCall(_ context.Context, e kobs.LLMCallEvent) {
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

func (o *Observer) OnToolCall(_ context.Context, e kobs.ToolCallEvent) {
	o.toolCallsTotal.WithLabelValues(e.ToolName, e.Risk, strconv.FormatBool(e.Error != nil)).Inc()
	o.toolDurationSecs.WithLabelValues(e.ToolName, e.Risk).Observe(e.Duration.Seconds())
}

func (o *Observer) OnSessionEvent(_ context.Context, e kobs.SessionEvent) {
	o.sessionsTotal.WithLabelValues(e.Type).Inc()
}

func (o *Observer) OnApproval(_ context.Context, e intr.ApprovalEvent) {
	o.approvalsTotal.WithLabelValues(string(e.Request.Kind), approvalDecision(e)).Inc()
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

func (o *Observer) OnError(_ context.Context, e kobs.ErrorEvent) {
	o.errorsTotal.WithLabelValues(e.Phase).Inc()
}
