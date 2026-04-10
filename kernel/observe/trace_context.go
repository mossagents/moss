package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// TraceContext carries distributed tracing identifiers through the agent pipeline.
// It is a lightweight struct not tied to OpenTelemetry and can be used independently.
type TraceContext struct {
	TraceID  string            `json:"trace_id"`
	SpanID   string            `json:"span_id"`
	ParentID string            `json:"parent_id,omitempty"`
	Baggage  map[string]string `json:"baggage,omitempty"`
}

type traceContextKey struct{}

// WithTraceContext stores a TraceContext in the Go context.
func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// TraceContextFrom retrieves the TraceContext from a Go context.
func TraceContextFrom(ctx context.Context) (TraceContext, bool) {
	tc, ok := ctx.Value(traceContextKey{}).(TraceContext)
	return tc, ok
}

// NewTraceID generates a random 32-hex-char trace ID.
func NewTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSpanID generates a random 16-hex-char span ID.
func NewSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ChildSpan creates a new TraceContext as a child of the current one.
// The new context inherits the same TraceID and Baggage, with the current
// SpanID becoming the ParentID and a fresh SpanID generated.
func (tc TraceContext) ChildSpan() TraceContext {
	child := TraceContext{
		TraceID:  tc.TraceID,
		SpanID:   NewSpanID(),
		ParentID: tc.SpanID,
	}
	if len(tc.Baggage) > 0 {
		child.Baggage = make(map[string]string, len(tc.Baggage))
		for k, v := range tc.Baggage {
			child.Baggage[k] = v
		}
	}
	return child
}
