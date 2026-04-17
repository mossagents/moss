package observe

import (
	"strings"
	"sync"
)

// NormalizedMetricsSnapshot captures unified success/latency/cost/tool-error counters.
type NormalizedMetricsSnapshot struct {
	RunTotal        float64
	RunSuccessTotal float64
	RunFailedTotal  float64

	LLMMSsum    float64
	LLMMSCount  float64
	ToolMSsum   float64
	ToolMSCount float64

	EstimatedCostUSDSum float64

	ToolCallsTotal  float64
	ToolErrorsTotal float64

	ContextCompactionsTotal                 float64
	ContextCompactionTokensReclaimedSum     float64
	ContextTrimRetriesTotal                 float64
	ContextTrimMessagesRemovedTotal         float64
	ContextNormalizeTotal                   float64
	ContextNormalizeDroppedToolResultsTotal float64
	ContextNormalizeSynthesizedResultsTotal float64

	GuardianReviewsTotal      float64
	GuardianAutoApprovedTotal float64
	GuardianFallbackTotal     float64
	GuardianErrorsTotal       float64
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
	llmAvgMs := 0.0
	if s.LLMMSCount > 0 {
		llmAvgMs = s.LLMMSsum / s.LLMMSCount
	}
	toolAvgMs := 0.0
	if s.ToolMSCount > 0 {
		toolAvgMs = s.ToolMSsum / s.ToolMSCount
	}
	guardianFallbackRate := 0.0
	guardianErrorRate := 0.0
	if s.GuardianReviewsTotal > 0 {
		guardianFallbackRate = s.GuardianFallbackTotal / s.GuardianReviewsTotal
		guardianErrorRate = s.GuardianErrorsTotal / s.GuardianReviewsTotal
	}
	return map[string]float64{
		"success.run_total":                            s.RunTotal,
		"success.run_success_total":                    s.RunSuccessTotal,
		"success.run_failed_total":                     s.RunFailedTotal,
		"success.rate":                                 successRate,
		"latency.llm_ms_sum":                           s.LLMMSsum,
		"latency.llm_ms_count":                         s.LLMMSCount,
		"latency.llm_avg_ms":                           llmAvgMs,
		"latency.tool_ms_sum":                          s.ToolMSsum,
		"latency.tool_ms_count":                        s.ToolMSCount,
		"latency.tool_avg_ms":                          toolAvgMs,
		"cost.estimated_usd_sum":                       s.EstimatedCostUSDSum,
		"tool_error.calls_total":                       s.ToolCallsTotal,
		"tool_error.errors_total":                      s.ToolErrorsTotal,
		"tool_error.rate":                              toolErrorRate,
		"context.compactions_total":                    s.ContextCompactionsTotal,
		"context.compaction_tokens_reclaimed_sum":      s.ContextCompactionTokensReclaimedSum,
		"context.trim_retry_total":                     s.ContextTrimRetriesTotal,
		"context.trim_removed_messages_total":          s.ContextTrimMessagesRemovedTotal,
		"context.normalize_total":                      s.ContextNormalizeTotal,
		"context.normalize_dropped_tool_results_total": s.ContextNormalizeDroppedToolResultsTotal,
		"context.normalize_synthesized_results_total":  s.ContextNormalizeSynthesizedResultsTotal,
		"guardian.review_total":                        s.GuardianReviewsTotal,
		"guardian.auto_approved_total":                 s.GuardianAutoApprovedTotal,
		"guardian.fallback_total":                      s.GuardianFallbackTotal,
		"guardian.error_total":                         s.GuardianErrorsTotal,
		"guardian.fallback_rate":                       guardianFallbackRate,
		"guardian.error_rate":                          guardianErrorRate,
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

func (a *MetricsAccumulator) ApplyExecutionEvent(e ExecutionEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch e.Type {
	case ExecutionContextCompacted:
		a.snapshot.ContextCompactionsTotal++
		before := metricFloat(e.Metadata, "tokens_before")
		after := metricFloat(e.Metadata, "tokens_after")
		if before > after {
			a.snapshot.ContextCompactionTokensReclaimedSum += before - after
		}
	case ExecutionContextTrimRetry:
		a.snapshot.ContextTrimRetriesTotal++
		a.snapshot.ContextTrimMessagesRemovedTotal += metricFloat(e.Metadata, "messages_removed")
	case ExecutionContextNormalized:
		a.snapshot.ContextNormalizeTotal++
		a.snapshot.ContextNormalizeDroppedToolResultsTotal += metricFloat(e.Metadata, "dropped_orphan_tool_results")
		a.snapshot.ContextNormalizeSynthesizedResultsTotal += metricFloat(e.Metadata, "synthesized_missing_tool_results")
	case ExecutionGuardianReviewed:
		a.snapshot.GuardianReviewsTotal++
		outcome := strings.ToLower(metricString(e.Metadata, "outcome"))
		switch outcome {
		case "auto_approved":
			a.snapshot.GuardianAutoApprovedTotal++
		case "fallback", "fallback_nil", "fallback_error":
			a.snapshot.GuardianFallbackTotal++
		}
		if strings.Contains(outcome, "error") {
			a.snapshot.GuardianErrorsTotal++
		}
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
	case EventKindExecution:
		if e.Execution != nil {
			a.ApplyExecutionEvent(*e.Execution)
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

func metricFloat(meta map[string]any, key string) float64 {
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

func metricString(meta map[string]any, key string) string {
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
