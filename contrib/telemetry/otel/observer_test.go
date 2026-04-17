package otel_test

import (
	"context"
	"errors"
	mossotel "github.com/mossagents/moss/contrib/telemetry/otel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"go.opentelemetry.io/otel/metric/noop"
	"testing"
	"time"
)

func newTestObs(t *testing.T) *mossotel.Observer {
	t.Helper()
	mp := noop.NewMeterProvider()
	obs, err := mossotel.New(mp.Meter("test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return obs
}

func TestObserverImplementsPortObserver(t *testing.T) {
	obs := newTestObs(t)
	var _ observe.Observer = obs
}

func TestObserver_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	ctx := context.Background()

	tests := []struct {
		name string
		run  func()
	}{
		{
			name: "llm call",
			run: func() {
				observe.ObserveLLMCall(ctx, obs, observe.LLMCallEvent{
					Model:            "gpt-4o",
					Duration:         300 * time.Millisecond,
					StopReason:       "end_turn",
					Usage:            model.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
					EstimatedCostUSD: 0.002,
				})
			},
		},
		{
			name: "llm call with error",
			run: func() {
				observe.ObserveLLMCall(ctx, obs, observe.LLMCallEvent{
					Model: "claude-3-5-sonnet",
					Error: errors.New("rate limit"),
				})
			},
		},
		{
			name: "tool call",
			run: func() {
				observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{
					ToolName: "bash",
					Risk:     "high",
					Duration: 200 * time.Millisecond,
					Error:    errors.New("exit 1"),
				})
			},
		},
		{
			name: "session event",
			run: func() {
				observe.ObserveSessionEvent(ctx, obs, observe.SessionEvent{Type: "completed"})
			},
		},
		{
			name: "approval pending",
			run: func() {
				observe.ObserveApproval(ctx, obs, io.ApprovalEvent{Request: io.ApprovalRequest{Kind: io.ApprovalKindTool}})
			},
		},
		{
			name: "approval resolved",
			run: func() {
				observe.ObserveApproval(ctx, obs, io.ApprovalEvent{
					Request:  io.ApprovalRequest{Kind: io.ApprovalKindTool},
					Decision: &io.ApprovalDecision{Approved: false},
				})
			},
		},
		{
			name: "error event",
			run: func() {
				observe.ObserveError(ctx, obs, observe.ErrorEvent{Phase: "loop"})
			},
		},
		{
			name: "execution event",
			run: func() {
				observe.ObserveExecutionEvent(ctx, obs, observe.ExecutionEvent{
					Type:     observe.ExecutionGuardianReviewed,
					Metadata: map[string]any{"outcome": "auto_approved"},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run()
		})
	}
}

func TestObserver_NormalizedMetricsMap(t *testing.T) {
	obs := newTestObs(t)
	ctx := context.Background()
	observe.ObserveLLMCall(ctx, obs, observe.LLMCallEvent{Duration: 100 * time.Millisecond, EstimatedCostUSD: 0.01})
	observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{ToolName: "read_file", Duration: 20 * time.Millisecond})
	observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{ToolName: "run_command", Duration: 30 * time.Millisecond, Error: errors.New("fail")})
	observe.ObserveSessionEvent(ctx, obs, observe.SessionEvent{Type: "completed"})
	observe.ObserveExecutionEvent(ctx, obs, observe.ExecutionEvent{
		Type: observe.ExecutionContextNormalized,
		Metadata: map[string]any{
			"dropped_orphan_tool_results":      1,
			"synthesized_missing_tool_results": 2,
		},
	})
	observe.ObserveExecutionEvent(ctx, obs, observe.ExecutionEvent{Type: observe.ExecutionGuardianReviewed, Metadata: map[string]any{"outcome": "fallback_error"}})

	m := obs.NormalizedMetricsMap()
	if m["success.run_total"] != 1 {
		t.Fatalf("run total = %v", m["success.run_total"])
	}
	if m["tool_error.calls_total"] != 2 || m["tool_error.errors_total"] != 1 {
		t.Fatalf("tool error counters mismatch: %+v", m)
	}
	if m["context.normalize_total"] != 1 || m["context.normalize_synthesized_results_total"] != 2 {
		t.Fatalf("execution metrics mismatch: %+v", m)
	}
	if m["guardian.error_rate"] != 1 {
		t.Fatalf("guardian error rate mismatch: %+v", m)
	}
}
