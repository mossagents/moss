package observe

import "context"

// MetricsObserver adapts MetricsAccumulator to the Observer interface so
// callers can collect normalized metrics from real kernel executions.
type MetricsObserver struct {
	NoOpObserver

	metrics *MetricsAccumulator
}

// NewMetricsObserver returns an Observer backed by a MetricsAccumulator.
// When acc is nil, a fresh accumulator is allocated.
func NewMetricsObserver(acc *MetricsAccumulator) *MetricsObserver {
	if acc == nil {
		acc = &MetricsAccumulator{}
	}
	return &MetricsObserver{metrics: acc}
}

func (o *MetricsObserver) OnLLMCall(_ context.Context, e LLMCallEvent) {
	if o == nil || o.metrics == nil {
		return
	}
	o.metrics.ApplyLLMCall(e)
}

func (o *MetricsObserver) OnToolCall(_ context.Context, e ToolCallEvent) {
	if o == nil || o.metrics == nil {
		return
	}
	o.metrics.ApplyToolCall(e)
}

func (o *MetricsObserver) OnExecutionEvent(_ context.Context, e ExecutionEvent) {
	if o == nil || o.metrics == nil {
		return
	}
	o.metrics.ApplyExecutionEvent(e)
}

func (o *MetricsObserver) OnSessionEvent(_ context.Context, e SessionEvent) {
	if o == nil || o.metrics == nil {
		return
	}
	o.metrics.ApplySessionEvent(e)
}

// Snapshot exposes the current normalized metrics snapshot.
func (o *MetricsObserver) Snapshot() NormalizedMetricsSnapshot {
	if o == nil || o.metrics == nil {
		return NormalizedMetricsSnapshot{}
	}
	return o.metrics.Snapshot()
}

// Map exposes the current normalized metrics in flattened key form.
func (o *MetricsObserver) Map() map[string]float64 {
	return o.Snapshot().Map()
}

var _ Observer = (*MetricsObserver)(nil)
