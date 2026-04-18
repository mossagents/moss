package product

import (
	"context"
	"errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"strings"
	"testing"
	"time"
)

func TestRunTraceRecorderApprovalResolvedStoresDecision(t *testing.T) {
	recorder := NewRunTraceRecorder()
	recorder.OnApproval(context.Background(), io.ApprovalEvent{
		SessionID: "sess-1",
		Type:      "resolved",
		Request: io.ApprovalRequest{
			ID:         "approval-1",
			ToolName:   "run_command",
			ReasonCode: "network",
		},
		Decision: &io.ApprovalDecision{
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
	approved, ok := trace.Timeline[0].Metadata["approved"].(bool)
	if !ok || approved {
		t.Fatalf("expected approval decision to be captured, got %+v", trace.Timeline[0].Metadata)
	}
	if got := trace.Timeline[0].Metadata["decision_reason"]; got != "no network" {
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
				{Kind: "approval", Type: "resolved", ToolName: "run_command", Metadata: map[string]any{"approved": false, "reason_code": "network"}},
				{Kind: "execution_event", Type: "llm_failover_switch", Model: "gpt-4o", Metadata: map[string]any{"candidate_model": "gpt-4o", "failover_to": "claude-sonnet"}},
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

func TestSelectKeyTraceEventsIncludesHostedToolLifecycle(t *testing.T) {
	events := SelectKeyTraceEvents(RunTrace{Timeline: []TraceEvent{{
		Kind:     "execution_event",
		Type:     "hosted_tool.completed",
		ToolName: "file_search_call",
		Metadata: map[string]any{"status": "completed"},
	}}}, 3)
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, "Hosted tool file_search_call completed") {
		t.Fatalf("expected hosted tool summary, got: %v", events)
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
				{Kind: "approval", Type: "resolved", ToolName: "run_command", Metadata: map[string]any{"approved": true, "source": "user"}},
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

// ── RecordingIO observer methods ──────────────────────────────────────────────

func TestRunTraceRecorder_OnToolCall(t *testing.T) {
	r := NewRunTraceRecorder()
	r.OnToolCall(context.Background(), observe.ToolCallEvent{
		SessionID: "sess-1",
		ToolName:  "read_file",
		Duration:  50 * time.Millisecond,
	})
	trace := r.Snapshot()
	if trace.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d, want 1", trace.ToolCalls)
	}
	if len(trace.Timeline) != 1 {
		t.Fatalf("timeline len=%d, want 1", len(trace.Timeline))
	}
	ev := trace.Timeline[0]
	if ev.Kind != "tool_call" || ev.ToolName != "read_file" || ev.DurationMS != 50 || ev.SessionID != "sess-1" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestRunTraceRecorder_OnToolCall_WithError(t *testing.T) {
	r := NewRunTraceRecorder()
	r.OnToolCall(context.Background(), observe.ToolCallEvent{
		ToolName: "write_file",
		Error:    errors.New("permission denied"),
	})
	trace := r.Snapshot()
	if trace.Timeline[0].Error != "permission denied" {
		t.Fatalf("error not recorded: %+v", trace.Timeline[0])
	}
}

func TestRunTraceRecorder_OnExecutionEvent(t *testing.T) {
	r := NewRunTraceRecorder()
	now := time.Now().UTC()
	r.OnExecutionEvent(context.Background(), observe.ExecutionEvent{
		EventID:   "evt-1",
		RunID:     "run-1",
		TurnID:    "turn-1",
		SessionID: "sess-1",
		Phase:     "execution",
		Actor:     "agent",
		Type:      "turn.plan_prepared",
		Model:     "gpt-5",
		ToolName:  "read_file",
		Duration:  100 * time.Millisecond,
		Timestamp: now,
		Metadata:  map[string]any{"k": "v"},
	})
	trace := r.Snapshot()
	if len(trace.Timeline) != 1 {
		t.Fatalf("timeline len=%d, want 1", len(trace.Timeline))
	}
	ev := trace.Timeline[0]
	if ev.Kind != "execution_event" || ev.Type != "turn.plan_prepared" || ev.EventID != "evt-1" {
		t.Fatalf("unexpected execution event: %+v", ev)
	}
	if ev.DurationMS != 100 {
		t.Fatalf("DurationMS=%d, want 100", ev.DurationMS)
	}
}

func TestRunTraceRecorder_OnSessionEvent(t *testing.T) {
	r := NewRunTraceRecorder()
	r.OnSessionEvent(context.Background(), observe.SessionEvent{
		SessionID: "sess-1",
		Type:      "running",
	})
	trace := r.Snapshot()
	ev := trace.Timeline[0]
	if ev.Kind != "session" || ev.Type != "running" || ev.SessionID != "sess-1" {
		t.Fatalf("unexpected session event: %+v", ev)
	}
}

func TestRunTraceRecorder_OnError(t *testing.T) {
	r := NewRunTraceRecorder()
	r.OnError(context.Background(), observe.ErrorEvent{
		SessionID: "sess-1",
		Phase:     "execution",
		Message:   "something went wrong",
	})
	trace := r.Snapshot()
	ev := trace.Timeline[0]
	if ev.Kind != "error" || ev.Type != "execution" || ev.Error != "something went wrong" {
		t.Fatalf("unexpected error event: %+v", ev)
	}
}

// ── pricingObserver passthrough ───────────────────────────────────────────────

type mockObserver struct {
	toolCalls       int
	executionEvents int
	approvals       int
	sessionEvents   int
	errorEvents     int
}

func (m *mockObserver) OnLLMCall(_ context.Context, _ observe.LLMCallEvent)   {}
func (m *mockObserver) OnToolCall(_ context.Context, _ observe.ToolCallEvent) { m.toolCalls++ }
func (m *mockObserver) OnExecutionEvent(_ context.Context, _ observe.ExecutionEvent) {
	m.executionEvents++
}
func (m *mockObserver) OnApproval(_ context.Context, _ io.ApprovalEvent)         { m.approvals++ }
func (m *mockObserver) OnSessionEvent(_ context.Context, _ observe.SessionEvent) { m.sessionEvents++ }
func (m *mockObserver) OnError(_ context.Context, _ observe.ErrorEvent)          { m.errorEvents++ }

func TestNewPricingObserver_NilNext(t *testing.T) {
	if got := NewPricingObserver(nil, nil); got != nil {
		t.Error("expected nil when next is nil")
	}
}

func TestNewPricingObserver_NilCatalog(t *testing.T) {
	m := &mockObserver{}
	got := NewPricingObserver(nil, m)
	if got != m {
		t.Error("expected to return next unchanged when catalog is nil")
	}
}

func TestPricingObserver_Passthrough(t *testing.T) {
	m := &mockObserver{}
	catalog := &PricingCatalog{Models: map[string]ModelPricing{}}
	obs := NewPricingObserver(catalog, m)

	ctx := context.Background()
	obs.OnToolCall(ctx, observe.ToolCallEvent{ToolName: "read_file"})
	obs.OnExecutionEvent(ctx, observe.ExecutionEvent{})
	obs.OnApproval(ctx, io.ApprovalEvent{})
	obs.OnSessionEvent(ctx, observe.SessionEvent{})
	obs.OnError(ctx, observe.ErrorEvent{})

	if m.toolCalls != 1 {
		t.Errorf("toolCalls=%d, want 1", m.toolCalls)
	}
	if m.executionEvents != 1 {
		t.Errorf("executionEvents=%d, want 1", m.executionEvents)
	}
	if m.approvals != 1 {
		t.Errorf("approvals=%d, want 1", m.approvals)
	}
	if m.sessionEvents != 1 {
		t.Errorf("sessionEvents=%d, want 1", m.sessionEvents)
	}
	if m.errorEvents != 1 {
		t.Errorf("errorEvents=%d, want 1", m.errorEvents)
	}
}

// ── SelectKeyTraceEvents ──────────────────────────────────────────────────────

func TestSelectKeyTraceEvents_LimitZero(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{{Kind: "error", Error: "boom"}}}
	if got := SelectKeyTraceEvents(trace, 0); got != nil {
		t.Fatalf("expected nil for limit=0, got %v", got)
	}
}

func TestSelectKeyTraceEvents_Empty(t *testing.T) {
	if got := SelectKeyTraceEvents(RunTrace{}, 5); len(got) != 0 {
		t.Fatalf("expected empty for empty trace, got %v", got)
	}
}

func TestSelectKeyTraceEvents_ErrorPriority(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{
		{Kind: "approval", Type: "resolved", ToolName: "cmd", Metadata: map[string]any{"approved": false}},
		{Kind: "error", Error: "fatal error", Type: "execution"},
	}}
	got := SelectKeyTraceEvents(trace, 1)
	if len(got) != 1 || !strings.Contains(got[0], "fatal error") {
		t.Fatalf("expected error event first, got %v", got)
	}
}

func TestSelectKeyTraceEvents_AllTypes(t *testing.T) {
	trace := RunTrace{
		Timeline: []TraceEvent{
			{Kind: "error", Error: "disk full"},
			{Kind: "approval", Type: "requested", ToolName: "shell", Metadata: map[string]any{"reason_code": "exec"}},
			{Kind: "execution_event", Type: "turn.plan_prepared", Metadata: map[string]any{"instruction_profile": "default", "model_lane": "fast"}},
			{Kind: "execution_event", Type: "model.route_planned", Metadata: map[string]any{"lane": "fast"}},
			{Kind: "execution_event", Type: "tool.route_planned", Metadata: map[string]any{"visible_tools_count": 3, "hidden_tools_count": 0, "approval_tools_count": 1}},
			{Kind: "execution_event", Type: "llm_failover_exhausted"},
			{Kind: "execution_event", Type: "llm_failover_switch", Model: "gpt-5", Metadata: map[string]any{"candidate_model": "gpt-5", "failover_to": "claude"}},
			{Kind: "llm_call", Error: "timeout", Model: "gpt-5", DurationMS: 3000},
			{Kind: "tool_call", Error: "denied", ToolName: "run_command", DurationMS: 50},
			{Kind: "execution_event", Type: "tool.completed", ToolName: "search", Metadata: map[string]any{"degraded": true, "enforcement": "allow"}},
			{Kind: "tool_call", ToolName: "slow_op", DurationMS: 5000},
		},
	}
	got := SelectKeyTraceEvents(trace, 20)
	joined := strings.Join(got, " | ")
	for _, want := range []string{"disk full", "Approval required for shell", "Turn plan prepared", "Model route planned", "Tool route planned", "failover exhausted", "Failover switched"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in: %s", want, joined)
		}
	}
}

func TestSelectKeyTraceEvents_Dedup(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{
		{Kind: "error", Error: "same error"},
		{Kind: "error", Error: "same error"},
	}}
	got := SelectKeyTraceEvents(trace, 10)
	if len(got) != 1 {
		t.Fatalf("expected dedup, got %d results: %v", len(got), got)
	}
}

// ── summarizeApprovalEvent ────────────────────────────────────────────────────

func TestSummarizeApprovalEvent(t *testing.T) {
	cases := []struct {
		event TraceEvent
		want  string
	}{
		{
			TraceEvent{Type: "requested", ToolName: "shell"},
			"Approval required for shell",
		},
		{
			TraceEvent{Type: "requested", ToolName: "shell", Metadata: map[string]any{"reason_code": "exec"}},
			"Approval required for shell (exec)",
		},
		{
			TraceEvent{Type: "resolved", ToolName: "shell", Metadata: map[string]any{"approved": true}},
			"Approval granted for shell",
		},
		{
			TraceEvent{Type: "resolved", ToolName: "shell", Metadata: map[string]any{"approved": false}},
			"Approval denied for shell",
		},
		{
			TraceEvent{Type: "resolved", ToolName: "shell", Metadata: map[string]any{"approved": false, "reason_code": "network"}},
			"Approval denied for shell (network)",
		},
		{
			TraceEvent{Type: "resolved", ToolName: "shell"},
			"Approval resolved for shell",
		},
		{
			TraceEvent{Type: "other", ToolName: "shell"},
			"",
		},
	}
	for _, tc := range cases {
		got := summarizeApprovalEvent(tc.event)
		if got != tc.want {
			t.Errorf("summarizeApprovalEvent(%q, tool=%q) = %q, want %q", tc.event.Type, tc.event.ToolName, got, tc.want)
		}
	}
}

// ── summarizeSlowestTraceEvent ────────────────────────────────────────────────

func TestSummarizeSlowestTraceEvent_NoEvents(t *testing.T) {
	_, ok := summarizeSlowestTraceEvent(RunTrace{})
	if ok {
		t.Error("expected false for empty trace")
	}
}

func TestSummarizeSlowestTraceEvent_SkipsErrorEvents(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{
		{Kind: "tool_call", ToolName: "tool1", DurationMS: 5000, Error: "failed"},
	}}
	_, ok := summarizeSlowestTraceEvent(trace)
	if ok {
		t.Error("expected false when all events have errors")
	}
}

func TestSummarizeSlowestTraceEvent_ToolCall(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{
		{Kind: "tool_call", ToolName: "fast_tool", DurationMS: 100},
		{Kind: "tool_call", ToolName: "slow_tool", DurationMS: 5000},
	}}
	summary, ok := summarizeSlowestTraceEvent(trace)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(summary.message, "slow_tool") || !strings.Contains(summary.message, "Slowest tool") {
		t.Fatalf("unexpected message: %q", summary.message)
	}
}

func TestSummarizeSlowestTraceEvent_LLMCall(t *testing.T) {
	trace := RunTrace{Timeline: []TraceEvent{
		{Kind: "llm_call", Model: "gpt-5", DurationMS: 3000},
	}}
	summary, ok := summarizeSlowestTraceEvent(trace)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(summary.message, "Slowest LLM call") || !strings.Contains(summary.message, "gpt-5") {
		t.Fatalf("unexpected message: %q", summary.message)
	}
}

// ── formatDurationSuffix ──────────────────────────────────────────────────────

func TestFormatDurationSuffix(t *testing.T) {
	if got := formatDurationSuffix(0); got != "" {
		t.Fatalf("expected empty for 0ms, got %q", got)
	}
	if got := formatDurationSuffix(-1); got != "" {
		t.Fatalf("expected empty for negative, got %q", got)
	}
	got := formatDurationSuffix(500)
	if !strings.HasPrefix(got, " (") || !strings.HasSuffix(got, ")") {
		t.Fatalf("unexpected suffix format for 500ms: %q", got)
	}
	if !strings.Contains(got, "500ms") {
		t.Fatalf("expected 500ms in suffix, got %q", got)
	}
}

// ── intValue ──────────────────────────────────────────────────────────────────

func TestIntValue(t *testing.T) {
	if got := intValue(nil, "k"); got != 0 {
		t.Errorf("nil map: want 0, got %d", got)
	}
	if got := intValue(map[string]any{}, "k"); got != 0 {
		t.Errorf("missing key: want 0, got %d", got)
	}
	if got := intValue(map[string]any{"k": 42}, "k"); got != 42 {
		t.Errorf("int: want 42, got %d", got)
	}
	if got := intValue(map[string]any{"k": int64(7)}, "k"); got != 7 {
		t.Errorf("int64: want 7, got %d", got)
	}
	if got := intValue(map[string]any{"k": float64(3.9)}, "k"); got != 3 {
		t.Errorf("float64: want 3, got %d", got)
	}
	if got := intValue(map[string]any{"k": "not-an-int"}, "k"); got != 0 {
		t.Errorf("string: want 0, got %d", got)
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
					Metadata: map[string]any{
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
					Metadata: map[string]any{
						"visible_tools":  []string{"read_file", "view"},
						"hidden_tools":   []string{"write_file"},
						"approval_tools": []string{"run_command"},
					},
				},
				{
					Kind:  "execution_event",
					Type:  "model.route_planned",
					Model: "gpt-5",
					Metadata: map[string]any{
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
