package events

import (
	"testing"
	"time"
)

func TestBusPublish(t *testing.T) {
	bus := NewBus()
	var received []Event

	bus.Subscribe(func(e Event) {
		received = append(received, e)
	})

	e := Event{
		EventID:   "1",
		Type:      EventRunStarted,
		RunID:     "run-1",
		Timestamp: time.Now(),
		Payload:   map[string]any{"goal": "test"},
	}
	bus.Publish(e)

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].EventID != "1" {
		t.Errorf("expected EventID 1, got %s", received[0].EventID)
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	count := 0

	bus.Subscribe(func(e Event) { count++ })
	bus.Subscribe(func(e Event) { count++ })

	bus.Publish(Event{EventID: "1", Type: EventRunStarted, Timestamp: time.Now()})

	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}
