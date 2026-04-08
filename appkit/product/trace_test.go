package product

import (
	"context"
	intr "github.com/mossagents/moss/kernel/io"
	"strings"
	"testing"
)

func TestRunTraceRecorderApprovalResolvedStoresDecision(t *testing.T) {
	recorder := NewRunTraceRecorder()
	recorder.OnApproval(context.Background(), intr.ApprovalEvent{
		SessionID: "sess-1",
		Type:      "resolved",
		Request: intr.ApprovalRequest{
			ID:         "approval-1",
			ToolName:   "run_command",
			ReasonCode: "network",
		},
		Decision: &intr.ApprovalDecision{
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

func TestRenderRunTraceDetailIncludesPlanningEvents(t *testing.T) {
	detail := RenderRunTraceDetail(RunTraceSummary{
		Status: "completed",
		Steps:  1,
		Trace: RunTrace{
			Timeline: []TraceEvent{
				{
					Kind: "execution_event",
					Type: "turn.plan_prepared",
					Data: map[string]any{
						"iteration":            1,
						"instruction_profile":  "planning",
						"model_lane":           "reasoning",
						"visible_tools_count":  2,
						"hidden_tools_count":   1,
						"approval_tools_count": 1,
					},
				},
				{
					Kind: "execution_event",
					Type: "tool.route_planned",
					Data: map[string]any{
						"visible_tools":  []string{"read_file", "view"},
						"hidden_tools":   []string{"write_file"},
						"approval_tools": []string{"run_command"},
					},
				},
				{
					Kind:  "execution_event",
					Type:  "model.route_planned",
					Model: "gpt-5",
					Data: map[string]any{
						"lane":         "reasoning",
						"reason_codes": []string{"planning_mode"},
						"capabilities": []string{"reasoning"},
					},
				},
			},
		},
	}, 10)

	for _, want := range []string{
		"[execution] turn.plan_prepared iteration=1 instruction_profile=planning model_lane=reasoning visible_tools_count=2 hidden_tools_count=1 approval_tools_count=1",
		"[execution] tool.route_planned visible_tools=read_file,view hidden_tools=write_file approval_tools=run_command",
		"[execution] model.route_planned model=gpt-5 lane=reasoning reason_codes=planning_mode capabilities=reasoning",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}
