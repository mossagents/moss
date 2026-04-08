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

