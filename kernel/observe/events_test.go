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
