package product

import (
	"context"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestRunTraceRecorderApprovalResolvedStoresDecision(t *testing.T) {
	recorder := NewRunTraceRecorder()
	recorder.OnApproval(context.Background(), port.ApprovalEvent{
		SessionID: "sess-1",
		Type:      "resolved",
		Request: port.ApprovalRequest{
			ID:         "approval-1",
			ToolName:   "run_command",
			ReasonCode: "network",
		},
		Decision: &port.ApprovalDecision{
			RequestID: "approval-1",
			Approved:  false,
			Source:    "user",
			Reason:    "no network",
		},
	})

	trace := recorder.Snapshot()
	if len(trace.Timeline) != 1 {
		t.Fatalf("timeline length=%d, want 1", len(trace.Timeline))
	}
	approved, ok := trace.Timeline[0].Data["approved"].(bool)
	if !ok || approved {
		t.Fatalf("expected approval decision to be captured, got %+v", trace.Timeline[0].Data)
	}
	if got := trace.Timeline[0].Data["decision_reason"]; got != "no network" {
		t.Fatalf("decision_reason=%v, want no network", got)
	}
}

func TestRenderRunTraceSummaryIncludesTotalsAndKeyEvents(t *testing.T) {
	summary := RenderRunTraceSummary(RunTraceSummary{
		Status: "failed",
		Steps:  4,
		Error:  "run failed",
		Trace: RunTrace{
			PromptTokens:     120,
			CompletionTokens: 80,
			TotalTokens:      200,
			EstimatedCostUSD: 0.125,
			LLMCalls:         2,
			ToolCalls:        1,
			Timeline: []TraceEvent{
				{Kind: "approval", Type: "resolved", ToolName: "run_command", Data: map[string]any{"approved": false, "reason_code": "network"}},
				{Kind: "execution_event", Type: "llm_failover_switch", Model: "gpt-4o", Data: map[string]any{"candidate_model": "gpt-4o", "failover_to": "claude-sonnet"}},
				{Kind: "tool_call", ToolName: "run_command", DurationMS: 900},
			},
		},
		CostAvailable: true,
	})

	for _, want := range []string{
		"Run summary:",
		"status=failed",
		"steps=4",
		"llm=2",
		"tools=1",
		"tokens=200",
		"cost=$0.125000",
		"error=run failed",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "\n") {
		t.Fatalf("expected single-line summary, got:\n%s", summary)
	}
}

func TestRenderRunTraceSummaryShowsUnavailableCostWhenMissing(t *testing.T) {
	summary := RenderRunTraceSummary(RunTraceSummary{
		Status: "completed",
		Steps:  1,
		Trace: RunTrace{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			LLMCalls:         1,
		},
	})
	if !strings.Contains(summary, "cost=n/a") {
		t.Fatalf("expected unavailable cost fallback, got:\n%s", summary)
	}
}

func TestRenderRunTraceDetailIncludesTimelineAndLimit(t *testing.T) {
	detail := RenderRunTraceDetail(RunTraceSummary{
		Status: "completed",
		Steps:  3,
		Trace: RunTrace{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			LLMCalls:         1,
			ToolCalls:        1,
			Timeline: []TraceEvent{
				{Kind: "session", Type: "running"},
				{Kind: "approval", Type: "resolved", ToolName: "run_command", Data: map[string]any{"approved": true, "source": "user"}},
				{Kind: "llm_call", Model: "gpt-5", Type: "tool_use", DurationMS: 120, TotalTokens: 15},
				{Kind: "tool_call", ToolName: "run_command", DurationMS: 900},
			},
		},
	}, 2)

	for _, want := range []string{
		"Last run trace:",
		"timeline: showing 2 of 4 events",
		"[llm] model=gpt-5 stop=tool_use duration=120ms tokens=15",
		"[tool] run_command duration=900ms",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "[session] running") {
		t.Fatalf("expected detail limit to show only recent events:\n%s", detail)
	}
}
