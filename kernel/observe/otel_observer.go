// Package observe provides OpenTelemetry-based observability for the Moss agent kernel.
package observe

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"time"
)

// OTelObserver creates OTel spans from kernel Observer events.
// Spans are created post-hoc using timing data embedded in each event,
// so no span context needs to be threaded through the kernel middleware chain.
//
// Users are responsible for configuring a TracerProvider (e.g. Jaeger, OTLP, Zipkin).
// Wire it in via otel.SetTracerProvider before creating this observer.
type OTelObserver struct {
	NoOpObserver
	tracer trace.Tracer
}

// NewOTelObserver creates an OTelObserver using the given TracerProvider.
//
// Example:
//
//	tp := otlptracegrpc.NewUnstarted(...)
//	obs := observe.NewOTelObserver(tp)
func NewOTelObserver(tp trace.TracerProvider) *OTelObserver {
	return &OTelObserver{
		tracer: tp.Tracer("moss", trace.WithInstrumentationVersion("0.1.0")),
	}
}

// OnLLMCall creates a backdated OTel span for the completed LLM call.
func (o *OTelObserver) OnLLMCall(ctx context.Context, e LLMCallEvent) {
	startTime := e.StartedAt
	if startTime.IsZero() {
		startTime = time.Now().Add(-e.Duration)
	}
	endTime := startTime.Add(e.Duration)

	_, span := o.tracer.Start(ctx, "moss.llm.call",
		trace.WithTimestamp(startTime),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("session.id", e.SessionID),
			attribute.String("ai.model", e.Model),
			attribute.Int("ai.usage.prompt_tokens", e.Usage.PromptTokens),
			attribute.Int("ai.usage.completion_tokens", e.Usage.CompletionTokens),
			attribute.String("ai.stop_reason", e.StopReason),
			attribute.Bool("ai.streamed", e.Streamed),
			attribute.Float64("ai.estimated_cost_usd", e.EstimatedCostUSD),
		),
	)
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End(trace.WithTimestamp(endTime))
}

// OnToolCall creates a backdated OTel span for the completed tool call.
func (o *OTelObserver) OnToolCall(ctx context.Context, e ToolCallEvent) {
	startTime := e.StartedAt
	if startTime.IsZero() {
		startTime = time.Now().Add(-e.Duration)
	}
	endTime := startTime.Add(e.Duration)

	_, span := o.tracer.Start(ctx, "moss.tool.call",
		trace.WithTimestamp(startTime),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("session.id", e.SessionID),
			attribute.String("tool.name", e.ToolName),
			attribute.String("tool.risk", e.Risk),
		),
	)
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End(trace.WithTimestamp(endTime))
}

// OnSessionEvent creates a span event for session lifecycle changes.
func (o *OTelObserver) OnSessionEvent(ctx context.Context, e SessionEvent) {
	now := time.Now()
	_, span := o.tracer.Start(ctx, fmt.Sprintf("moss.session.%s", e.Type),
		trace.WithTimestamp(now),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("session.id", e.SessionID),
			attribute.String("session.type", e.Type),
		),
	)
	span.End(trace.WithTimestamp(now))
}

// OnError creates a span event for unexpected errors.
func (o *OTelObserver) OnError(ctx context.Context, e ErrorEvent) {
	now := time.Now()
	_, span := o.tracer.Start(ctx, "moss.error",
		trace.WithTimestamp(now),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("session.id", e.SessionID),
			attribute.String("error.phase", e.Phase),
			attribute.String("error.message", e.Message),
		),
	)
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End(trace.WithTimestamp(now))
}

// ensure OTelObserver satisfies Observer at compile time
var _ Observer = (*OTelObserver)(nil)
