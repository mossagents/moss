package observe

import (
	"context"
	"testing"
)

func TestNewTraceID(t *testing.T) {
	id := NewTraceID()
	if len(id) != 32 {
		t.Fatalf("TraceID length = %d, want 32", len(id))
	}
	id2 := NewTraceID()
	if id == id2 {
		t.Fatal("two TraceIDs should not be equal")
	}
}

func TestNewSpanID(t *testing.T) {
	id := NewSpanID()
	if len(id) != 16 {
		t.Fatalf("SpanID length = %d, want 16", len(id))
	}
	id2 := NewSpanID()
	if id == id2 {
		t.Fatal("two SpanIDs should not be equal")
	}
}

func TestWithTraceContextAndFrom(t *testing.T) {
	tc := TraceContext{
		TraceID:  NewTraceID(),
		SpanID:   NewSpanID(),
		ParentID: "",
		Baggage:  map[string]string{"user": "alice"},
	}
	ctx := WithTraceContext(context.Background(), tc)
	got, ok := TraceContextFrom(ctx)
	if !ok {
		t.Fatal("TraceContextFrom returned false")
	}
	if got.TraceID != tc.TraceID {
		t.Fatalf("TraceID = %q, want %q", got.TraceID, tc.TraceID)
	}
	if got.SpanID != tc.SpanID {
		t.Fatalf("SpanID = %q, want %q", got.SpanID, tc.SpanID)
	}
	if got.Baggage["user"] != "alice" {
		t.Fatalf("Baggage[user] = %q, want %q", got.Baggage["user"], "alice")
	}
}

func TestTraceContextFromMissing(t *testing.T) {
	_, ok := TraceContextFrom(context.Background())
	if ok {
		t.Fatal("TraceContextFrom should return false on empty context")
	}
}

func TestChildSpan(t *testing.T) {
	parent := TraceContext{
		TraceID: NewTraceID(),
		SpanID:  NewSpanID(),
		Baggage: map[string]string{"env": "test"},
	}
	child := parent.ChildSpan()
	if child.TraceID != parent.TraceID {
		t.Fatalf("child TraceID = %q, want %q", child.TraceID, parent.TraceID)
	}
	if child.ParentID != parent.SpanID {
		t.Fatalf("child ParentID = %q, want %q", child.ParentID, parent.SpanID)
	}
	if child.SpanID == parent.SpanID {
		t.Fatal("child SpanID should differ from parent SpanID")
	}
	if len(child.SpanID) != 16 {
		t.Fatalf("child SpanID length = %d, want 16", len(child.SpanID))
	}
	if child.Baggage["env"] != "test" {
		t.Fatalf("child Baggage[env] = %q, want %q", child.Baggage["env"], "test")
	}
	// Verify baggage is a copy, not shared.
	child.Baggage["extra"] = "x"
	if _, ok := parent.Baggage["extra"]; ok {
		t.Fatal("child baggage mutation should not affect parent")
	}
}

func TestChildSpanNilBaggage(t *testing.T) {
	parent := TraceContext{
		TraceID: NewTraceID(),
		SpanID:  NewSpanID(),
	}
	child := parent.ChildSpan()
	if child.Baggage != nil {
		t.Fatal("child Baggage should be nil when parent has no baggage")
	}
}
