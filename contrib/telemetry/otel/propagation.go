// Package otel (propagation) provides W3C TraceContext propagation helpers for
// the moss gateway and HTTP adapters.
//
// Wire-up example:
//
//	// 1. Set global OTEL propagator (call once at program start)
//	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
//	    propagation.TraceContext{}, propagation.Baggage{},
//	))
//
//	// 2. Extract incoming trace context in the Gateway
//	gw := gateway.New(k, router, gateway.WithTraceExtractor(mossotel.MetadataExtractor()))
//
//	// 3. Inject trace context into outbound LLM / embedding HTTP calls
//	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
//	httpClient := &http.Client{Transport: transport}
//	llmAdapter := claude.NewWithHTTPClient(apiKey, httpClient)

package otel

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// MetadataCarrier adapts map[string]any to the OTEL TextMapCarrier interface,
// allowing W3C traceparent/tracestate headers to be extracted from or injected
// into the Metadata fields of gateway InboundMessage / OutboundMessage.
// Only string-typed values are visible to the carrier; other types are ignored.
type MetadataCarrier map[string]any

// Get returns the string value for the given key, or "" if absent or non-string.
func (c MetadataCarrier) Get(key string) string {
	v, _ := c[key].(string)
	return v
}

// Set stores val as a string in the underlying map.
func (c MetadataCarrier) Set(key, val string) { c[key] = val }

// Keys returns all keys whose value is a string (propagation headers only).
func (c MetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k, v := range c {
		if _, ok := v.(string); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// ExtractFromMetadata extracts the W3C trace context from a message metadata map
// into ctx using the global OTEL text-map propagator.
// Returns ctx unchanged when metadata is nil or empty.
func ExtractFromMetadata(ctx context.Context, metadata map[string]any) context.Context {
	if len(metadata) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, MetadataCarrier(metadata))
}

// InjectToMetadata injects the active trace context from ctx into metadata using
// the global OTEL text-map propagator. If metadata is nil a new map is created.
// The (possibly new) map is returned; callers must use the return value.
func InjectToMetadata(ctx context.Context, metadata map[string]any) map[string]any {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	otel.GetTextMapPropagator().Inject(ctx, MetadataCarrier(metadata))
	return metadata
}

// MetadataExtractor returns a function compatible with gateway.Config.TraceExtractor.
// Wire it via:
//
//	gateway.New(k, router, gateway.WithTraceExtractor(mossotel.MetadataExtractor()))
func MetadataExtractor() func(context.Context, map[string]any) context.Context {
	return ExtractFromMetadata
}

// TraceTransport wraps an http.RoundTripper and injects the active W3C trace
// context (traceparent / tracestate) into every outbound HTTP request.
// Use it to propagate spans through LLM API calls and embedding requests.
//
// Example:
//
//	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
//	httpClient := &http.Client{Transport: transport}
//	claudeAdapter := claude.NewWithHTTPClient(apiKey, httpClient)
type TraceTransport struct {
	// Base is the underlying RoundTripper. Defaults to http.DefaultTransport if nil.
	Base http.RoundTripper
	// Propagator overrides the global OTEL text-map propagator. nil = use global.
	Propagator propagation.TextMapPropagator
}

// RoundTrip injects trace headers then delegates to the Base transport.
func (t *TraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	prop := t.Propagator
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}
	// Clone request to avoid mutating the original.
	r2 := req.Clone(req.Context())
	prop.Inject(r2.Context(), propagation.HeaderCarrier(r2.Header))
	return base.RoundTrip(r2)
}
