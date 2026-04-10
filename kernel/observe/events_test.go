package observe

import (
	"context"
	"testing"
	"time"
)

type envelopeSpy struct {
	NoOpObserver
	envelopes []EventEnvelope
}

func (s *envelopeSpy) OnEvent(_ context.Context, e EventEnvelope) {
	s.envelopes = append(s.envelopes, e)
}

func TestObserveExecutionEventDispatchesEnvelopeToJoinedObservers(t *testing.T) {
	left := &envelopeSpy{}
	right := &envelopeSpy{}
	observer := JoinObservers(left, right)

	ObserveExecutionEvent(context.Background(), observer, ExecutionEvent{
		Type:      ExecutionToolCompleted,
		SessionID: "sess-1",
		RunID:     "run-1",
		TurnID:    "turn-1",
		Timestamp: time.Now().UTC(),
		ToolName:  "run_command",
	})

	if len(left.envelopes) != 1 || len(right.envelopes) != 1 {
		t.Fatalf("expected both observers to receive envelope, got left=%d right=%d", len(left.envelopes), len(right.envelopes))
	}
	if left.envelopes[0].Kind != EventKindTurn {
		t.Fatalf("expected execution tool event to surface as turn envelope, got %+v", left.envelopes[0])
	}
	if left.envelopes[0].Execution == nil || left.envelopes[0].Turn == nil {
		t.Fatalf("expected execution and turn payloads, got %+v", left.envelopes[0])
	}
}

// panicObserver panics on every event method to test panic recovery.
type panicObserver struct{ NoOpObserver }

func (panicObserver) OnLLMCall(_ context.Context, _ LLMCallEvent)          { panic("boom") }
func (panicObserver) OnToolCall(_ context.Context, _ ToolCallEvent)        { panic("boom") }
func (panicObserver) OnExecutionEvent(_ context.Context, _ ExecutionEvent) { panic("boom") }
func (panicObserver) OnSessionEvent(_ context.Context, _ SessionEvent)     { panic("boom") }
func (panicObserver) OnError(_ context.Context, _ ErrorEvent)              { panic("boom") }

func TestJoinedObserver_PanicRecovery(t *testing.T) {
	spy := &envelopeSpy{}
	observer := JoinObservers(panicObserver{}, spy)

	// panicking observer should not prevent the second observer from receiving events
	observer.OnLLMCall(context.Background(), LLMCallEvent{SessionID: "s1"})
	observer.OnToolCall(context.Background(), ToolCallEvent{SessionID: "s2"})
	observer.OnSessionEvent(context.Background(), SessionEvent{SessionID: "s3"})

	// spy should still be alive and receiving events
	ObserveExecutionEvent(context.Background(), observer, ExecutionEvent{
		Type:      ExecutionRunStarted,
		SessionID: "s4",
		Timestamp: time.Now().UTC(),
	})
	if len(spy.envelopes) != 1 {
		t.Fatalf("expected spy to receive 1 envelope despite panic, got %d", len(spy.envelopes))
	}
}
