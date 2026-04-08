package observe

import "sync"

// NormalizedMetricsSnapshot captures unified success/latency/cost/tool-error counters.
type NormalizedMetricsSnapshot struct {
	RunTotal        float64
	RunSuccessTotal float64
	RunFailedTotal  float64

	LLMMSsum   float64
	LLMMSCount float64
	ToolMSsum  float64
	ToolMSCount float64

	EstimatedCostUSDSum float64

	ToolCallsTotal  float64
	ToolErrorsTotal float64
}

// Map exports the snapshot using the standardized metric keys.
func (s NormalizedMetricsSnapshot) Map() map[string]float64 {
	successRate := 0.0
	if s.RunTotal > 0 {
		successRate = s.RunSuccessTotal / s.RunTotal
	}
	toolErrorRate := 0.0
	if s.ToolCallsTotal > 0 {
		toolErrorRate = s.ToolErrorsTotal / s.ToolCallsTotal
	}
	return map[string]float64{
		"success.run_total":         s.RunTotal,
		"success.run_success_total": s.RunSuccessTotal,
		"success.run_failed_total":  s.RunFailedTotal,
		"success.rate":              successRate,
		"latency.llm_ms_sum":        s.LLMMSsum,
		"latency.llm_ms_count":      s.LLMMSCount,
		"latency.tool_ms_sum":       s.ToolMSsum,
		"latency.tool_ms_count":     s.ToolMSCount,
		"cost.estimated_usd_sum":    s.EstimatedCostUSDSum,
		"tool_error.calls_total":    s.ToolCallsTotal,
		"tool_error.errors_total":   s.ToolErrorsTotal,
		"tool_error.rate":           toolErrorRate,
	}
}

// MetricsAccumulator incrementally builds normalized metric snapshots.
type MetricsAccumulator struct {
	mu       sync.Mutex
	snapshot NormalizedMetricsSnapshot
}

func (a *MetricsAccumulator) ApplyLLMCall(e LLMCallEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snapshot.LLMMSsum += float64(e.Duration.Milliseconds())
	a.snapshot.LLMMSCount++
	if e.EstimatedCostUSD > 0 {
		a.snapshot.EstimatedCostUSDSum += e.EstimatedCostUSD
	}
}

func (a *MetricsAccumulator) ApplyToolCall(e ToolCallEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snapshot.ToolMSsum += float64(e.Duration.Milliseconds())
	a.snapshot.ToolMSCount++
	a.snapshot.ToolCallsTotal++
	if e.Error != nil {
		a.snapshot.ToolErrorsTotal++
	}
}

func (a *MetricsAccumulator) ApplySessionEvent(e SessionEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch e.Type {
	case "completed", "failed", "cancelled":
		a.snapshot.RunTotal++
	}
	if e.Type == "completed" {
		a.snapshot.RunSuccessTotal++
	}
	if e.Type == "failed" || e.Type == "cancelled" {
		a.snapshot.RunFailedTotal++
	}
}

func (a *MetricsAccumulator) ApplyEnvelope(e EventEnvelope) {
	switch e.Kind {
	case EventKindLLM:
		if e.LLM != nil {
			a.ApplyLLMCall(*e.LLM)
		}
	case EventKindTool:
		if e.Tool != nil {
			a.ApplyToolCall(*e.Tool)
		}
	case EventKindSession:
		if e.Session != nil {
			a.ApplySessionEvent(*e.Session)
		}
	}
}

func (a *MetricsAccumulator) Snapshot() NormalizedMetricsSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshot
}

func (a *MetricsAccumulator) Map() map[string]float64 {
	return a.Snapshot().Map()
}

