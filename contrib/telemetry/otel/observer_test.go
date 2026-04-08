package otel_test

import (
	"context"
	"errors"
	mossotel "github.com/mossagents/moss/contrib/telemetry/otel"
	intr "github.com/mossagents/moss/kernel/io"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
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
	var _ kobs.Observer = obs
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
				kobs.ObserveLLMCall(ctx, obs, kobs.LLMCallEvent{
					Model:            "gpt-4o",
					Duration:         300 * time.Millisecond,
					StopReason:       "end_turn",
					Usage:            mdl.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
					EstimatedCostUSD: 0.002,
				})
			},
		},
		{
			name: "llm call with error",
			run: func() {
				kobs.ObserveLLMCall(ctx, obs, kobs.LLMCallEvent{
					Model: "claude-3-5-sonnet",
					Error: errors.New("rate limit"),
				})
			},
		},
		{
			name: "tool call",
			run: func() {
				kobs.ObserveToolCall(ctx, obs, kobs.ToolCallEvent{
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
				kobs.ObserveSessionEvent(ctx, obs, kobs.SessionEvent{Type: "completed"})
			},
		},
		{
			name: "approval pending",
			run: func() {
				kobs.ObserveApproval(ctx, obs, intr.ApprovalEvent{Request: intr.ApprovalRequest{Kind: intr.ApprovalKindTool}})
			},
		},
		{
			name: "approval resolved",
			run: func() {
				kobs.ObserveApproval(ctx, obs, intr.ApprovalEvent{
					Request:  intr.ApprovalRequest{Kind: intr.ApprovalKindTool},
					Decision: &intr.ApprovalDecision{Approved: false},
				})
			},
		},
		{
			name: "error event",
			run: func() {
				kobs.ObserveError(ctx, obs, kobs.ErrorEvent{Phase: "loop"})
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
	kobs.ObserveLLMCall(ctx, obs, kobs.LLMCallEvent{Duration: 100 * time.Millisecond, EstimatedCostUSD: 0.01})
	kobs.ObserveToolCall(ctx, obs, kobs.ToolCallEvent{ToolName: "read_file", Duration: 20 * time.Millisecond})
	kobs.ObserveToolCall(ctx, obs, kobs.ToolCallEvent{ToolName: "run_command", Duration: 30 * time.Millisecond, Error: errors.New("fail")})
	kobs.ObserveSessionEvent(ctx, obs, kobs.SessionEvent{Type: "completed"})

	m := obs.NormalizedMetricsMap()
	if m["success.run_total"] != 1 {
		t.Fatalf("run total = %v", m["success.run_total"])
	}
	if m["tool_error.calls_total"] != 2 || m["tool_error.errors_total"] != 1 {
		t.Fatalf("tool error counters mismatch: %+v", m)
	}
}

