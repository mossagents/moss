package observe

import (
	"errors"
	"testing"
	"time"
)

func TestMetricsAccumulator_Map(t *testing.T) {
	acc := &MetricsAccumulator{}
	acc.ApplyEnvelope(EnvelopeFromLLMCall(LLMCallEvent{Duration: 120 * time.Millisecond, EstimatedCostUSD: 0.01}))
	acc.ApplyEnvelope(EnvelopeFromToolCall(ToolCallEvent{ToolName: "read_file", Duration: 20 * time.Millisecond}))
	acc.ApplyEnvelope(EnvelopeFromToolCall(ToolCallEvent{ToolName: "run_command", Duration: 30 * time.Millisecond, Error: errors.New("boom")}))
	acc.ApplyEnvelope(EnvelopeFromSessionEvent(SessionEvent{Type: "completed"}))
	acc.ApplyEnvelope(EnvelopeFromSessionEvent(SessionEvent{Type: "failed"}))

	m := acc.Map()
	if m["success.run_total"] != 2 {
		t.Fatalf("run total = %v", m["success.run_total"])
	}
	if m["success.run_success_total"] != 1 {
		t.Fatalf("run success total = %v", m["success.run_success_total"])
	}
	if m["tool_error.calls_total"] != 2 {
		t.Fatalf("tool calls total = %v", m["tool_error.calls_total"])
	}
	if m["tool_error.errors_total"] != 1 {
		t.Fatalf("tool errors total = %v", m["tool_error.errors_total"])
	}
	if m["cost.estimated_usd_sum"] <= 0 {
		t.Fatalf("cost sum should be >0, got %v", m["cost.estimated_usd_sum"])
	}
}

func TestMetricsAccumulator_ExecutionMetrics(t *testing.T) {
	acc := &MetricsAccumulator{}
	acc.ApplyEnvelope(EnvelopeFromExecutionEvent(ExecutionEvent{
		Type:     ExecutionContextCompacted,
		Metadata: map[string]any{"tokens_before": 220, "tokens_after": 140},
	}))
	acc.ApplyEnvelope(EnvelopeFromExecutionEvent(ExecutionEvent{
		Type:     ExecutionContextTrimRetry,
		Metadata: map[string]any{"messages_removed": 2},
	}))
	acc.ApplyEnvelope(EnvelopeFromExecutionEvent(ExecutionEvent{
		Type: ExecutionContextNormalized,
		Metadata: map[string]any{
			"dropped_orphan_tool_results":      1,
			"synthesized_missing_tool_results": 2,
		},
	}))
	acc.ApplyEnvelope(EnvelopeFromExecutionEvent(ExecutionEvent{
		Type:     ExecutionGuardianReviewed,
		Metadata: map[string]any{"outcome": "auto_approved"},
	}))
	acc.ApplyEnvelope(EnvelopeFromExecutionEvent(ExecutionEvent{
		Type:     ExecutionGuardianReviewed,
		Metadata: map[string]any{"outcome": "fallback_error"},
	}))

	m := acc.Map()
	if m["context.compactions_total"] != 1 {
		t.Fatalf("context compactions = %v", m["context.compactions_total"])
	}
	if m["context.compaction_tokens_reclaimed_sum"] != 80 {
		t.Fatalf("context compaction reclaimed = %v", m["context.compaction_tokens_reclaimed_sum"])
	}
	if m["context.trim_retry_total"] != 1 {
		t.Fatalf("context trim retries = %v", m["context.trim_retry_total"])
	}
	if m["context.normalize_dropped_tool_results_total"] != 1 {
		t.Fatalf("context normalize dropped = %v", m["context.normalize_dropped_tool_results_total"])
	}
	if m["context.normalize_synthesized_results_total"] != 2 {
		t.Fatalf("context normalize synthesized = %v", m["context.normalize_synthesized_results_total"])
	}
	if m["guardian.review_total"] != 2 {
		t.Fatalf("guardian review total = %v", m["guardian.review_total"])
	}
	if m["guardian.auto_approved_total"] != 1 {
		t.Fatalf("guardian auto approved total = %v", m["guardian.auto_approved_total"])
	}
	if m["guardian.fallback_total"] != 1 {
		t.Fatalf("guardian fallback total = %v", m["guardian.fallback_total"])
	}
	if m["guardian.error_total"] != 1 {
		t.Fatalf("guardian error total = %v", m["guardian.error_total"])
	}
	if m["guardian.fallback_rate"] != 0.5 {
		t.Fatalf("guardian fallback rate = %v", m["guardian.fallback_rate"])
	}
	if m["guardian.error_rate"] != 0.5 {
		t.Fatalf("guardian error rate = %v", m["guardian.error_rate"])
	}
}
