package otel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	mossotel "github.com/mossagents/moss/contrib/telemetry/otel"
	"github.com/mossagents/moss/kernel/port"
	"go.opentelemetry.io/otel/metric/noop"
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
	var _ port.Observer = obs
}

func TestOnLLMCall_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnLLMCall(context.Background(), port.LLMCallEvent{
		Model:            "gpt-4o",
		Duration:         300 * time.Millisecond,
		StopReason:       "end_turn",
		Usage:            port.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
		EstimatedCostUSD: 0.002,
	})
}

func TestOnLLMCall_withError_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnLLMCall(context.Background(), port.LLMCallEvent{
		Model: "claude-3-5-sonnet",
		Error: errors.New("rate limit"),
	})
}

func TestOnToolCall_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnToolCall(context.Background(), port.ToolCallEvent{
		ToolName: "bash",
		Risk:     "high",
		Duration: 200 * time.Millisecond,
		Error:    errors.New("exit 1"),
	})
}

func TestOnSessionEvent_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnSessionEvent(context.Background(), port.SessionEvent{Type: "completed"})
}

func TestOnApproval_pending_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnApproval(context.Background(), port.ApprovalEvent{
		Request: port.ApprovalRequest{Kind: port.ApprovalKindTool},
	})
}

func TestOnApproval_resolved_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnApproval(context.Background(), port.ApprovalEvent{
		Request:  port.ApprovalRequest{Kind: port.ApprovalKindTool},
		Decision: &port.ApprovalDecision{Approved: false},
	})
}

func TestOnError_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	obs.OnError(context.Background(), port.ErrorEvent{Phase: "loop"})
}
