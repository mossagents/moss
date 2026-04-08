package metrics

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"time"
)

// Predefined metric names used by MetricsObserver.
const (
	MetricLLMCallsTotal    = "moss_llm_calls_total"
	MetricLLMErrorsTotal   = "moss_llm_errors_total"
	MetricLLMTokensTotal   = "moss_llm_tokens_total"
	MetricLLMDurationSecs  = "moss_llm_duration_seconds"
	MetricToolCallsTotal   = "moss_tool_calls_total"
	MetricToolErrorsTotal  = "moss_tool_errors_total"
	MetricToolDurationSecs = "moss_tool_duration_seconds"
)

// MetricsObserver implements kobs.Observer by recording LLM and tool metrics.
type MetricsObserver struct {
	kobs.NoOpObserver

	llmCalls     Counter
	llmErrors    Counter
	llmTokens    Counter
	llmDuration  Histogram
	toolCalls    Counter
	toolErrors   Counter
	toolDuration Histogram
}

// NewObserver creates a MetricsObserver backed by the given Collector.
func NewObserver(c Collector) *MetricsObserver {
	return &MetricsObserver{
		llmCalls:     c.Counter(MetricLLMCallsTotal),
		llmErrors:    c.Counter(MetricLLMErrorsTotal),
		llmTokens:    c.Counter(MetricLLMTokensTotal),
		llmDuration:  c.Histogram(MetricLLMDurationSecs),
		toolCalls:    c.Counter(MetricToolCallsTotal),
		toolErrors:   c.Counter(MetricToolErrorsTotal),
		toolDuration: c.Histogram(MetricToolDurationSecs),
	}
}

// OnLLMCall records LLM call metrics.
func (o *MetricsObserver) OnLLMCall(_ context.Context, e kobs.LLMCallEvent) {
	o.llmCalls.Inc()
	if e.Error != nil {
		o.llmErrors.Inc()
	}
	total := e.Usage.PromptTokens + e.Usage.CompletionTokens
	if total > 0 {
		o.llmTokens.Add(float64(total))
	}
	if e.Duration > 0 {
		o.llmDuration.Observe(e.Duration.Seconds())
	}
}

// OnToolCall records tool call metrics.
func (o *MetricsObserver) OnToolCall(_ context.Context, e kobs.ToolCallEvent) {
	o.toolCalls.Inc()
	if e.Error != nil {
		o.toolErrors.Inc()
	}
	if e.Duration > 0 {
		o.toolDuration.Observe(e.Duration.Seconds())
	}
}

// ensure MetricsObserver satisfies kobs.Observer at compile time
var _ kobs.Observer = (*MetricsObserver)(nil)

// TokenUsage convenience re-export for callers building test events.
type TokenUsage = mdl.TokenUsage

// NewLLMCallEvent constructs a kobs.LLMCallEvent for testing purposes.
func NewLLMCallEvent(sessionID, model string, start time.Time, dur time.Duration, promptToks, completionToks int, err error) kobs.LLMCallEvent {
	return kobs.LLMCallEvent{
		SessionID: sessionID,
		Model:     model,
		StartedAt: start,
		Duration:  dur,
		Usage: mdl.TokenUsage{
			PromptTokens:     promptToks,
			CompletionTokens: completionToks,
		},
		Error: err,
	}
}
