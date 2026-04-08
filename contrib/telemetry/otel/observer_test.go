package otel_test

import (
	"context"
	"errors"
	mossotel "github.com/mossagents/moss/contrib/telemetry/otel"
	intr "github.com/mossagents/moss/kernel/interaction"
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

func TestOnLLMCall_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnLLMCall(context.Background(), kobs.LLMCallEvent{
		Model:            "gpt-4o",
		Duration:         300 * time.Millisecond,
		StopReason:       "end_turn",
		Usage:            mdl.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
		EstimatedCostUSD: 0.002,
	})
}

func TestOnLLMCall_withError_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnLLMCall(context.Background(), kobs.LLMCallEvent{
		Model: "claude-3-5-sonnet",
		Error: errors.New("rate limit"),
	})
}

func TestOnToolCall_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnToolCall(context.Background(), kobs.ToolCallEvent{
		ToolName: "bash",
		Risk:     "high",
		Duration: 200 * time.Millisecond,
		Error:    errors.New("exit 1"),
	})
}

func TestOnSessionEvent_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnSessionEvent(context.Background(), kobs.SessionEvent{Type: "completed"})
}

func TestOnApproval_pending_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnApproval(context.Background(), intr.ApprovalEvent{
		Request: intr.ApprovalRequest{Kind: intr.ApprovalKindTool},
	})
}

func TestOnApproval_resolved_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnApproval(context.Background(), intr.ApprovalEvent{
		Request:  intr.ApprovalRequest{Kind: intr.ApprovalKindTool},
		Decision: &intr.ApprovalDecision{Approved: false},
	})
}

func TestOnError_noopDoesNotPanic(t *testing.T) {
	obs := newTestObs(t)
	kobs.OnError(context.Background(), kobs.ErrorEvent{Phase: "loop"})
}
