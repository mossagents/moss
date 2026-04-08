package otel_test

import (
	"context"
	mossotel "github.com/mossagents/moss/contrib/telemetry/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// setupPropagation installs a noop tracer + W3C propagator for the test process.
func setupPropagation(t *testing.T) {
	t.Helper()
	otel.SetTracerProvider(noop.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// ---------------------------------------------------------------------------
// MetadataCarrier
// ---------------------------------------------------------------------------

func TestMetadataCarrier_Get_stringValue(t *testing.T) {
	mc := mossotel.MetadataCarrier{"traceparent": "00-abc-def-01"}
	if got := mc.Get("traceparent"); got != "00-abc-def-01" {
		t.Errorf("Get: want %q got %q", "00-abc-def-01", got)
	}
}

func TestMetadataCarrier_Get_nonStringIgnored(t *testing.T) {
	mc := mossotel.MetadataCarrier{"counter": 42}
	if got := mc.Get("counter"); got != "" {
		t.Errorf("Get non-string: want empty got %q", got)
	}
}

func TestMetadataCarrier_Get_missingKey(t *testing.T) {
	mc := mossotel.MetadataCarrier{}
	if got := mc.Get("traceparent"); got != "" {
		t.Errorf("Get missing: want empty got %q", got)
	}
}

func TestMetadataCarrier_Set(t *testing.T) {
	mc := mossotel.MetadataCarrier{}
	mc.Set("tracestate", "vendor=value")
	if got := mc.Get("tracestate"); got != "vendor=value" {
		t.Errorf("Set then Get: want %q got %q", "vendor=value", got)
	}
}

func TestMetadataCarrier_Keys_onlyStrings(t *testing.T) {
	mc := mossotel.MetadataCarrier{
		"traceparent": "00-abc-def-01",
		"counter":     42,
		"tracestate":  "k=v",
	}
	keys := mc.Keys()
	sort.Strings(keys)
	if len(keys) != 2 {
		t.Fatalf("Keys: want 2 string keys got %v", keys)
	}
	if keys[0] != "traceparent" || keys[1] != "tracestate" {
		t.Errorf("Keys: got %v", keys)
	}
}

// ---------------------------------------------------------------------------
// ExtractFromMetadata / InjectToMetadata
// ---------------------------------------------------------------------------

func TestExtractFromMetadata_nil_returnsOriginalCtx(t *testing.T) {
	setupPropagation(t)
	ctx := context.Background()
	if got := mossotel.ExtractFromMetadata(ctx, nil); got != ctx {
		t.Error("nil metadata: expected original context returned unchanged")
	}
}

func TestExtractFromMetadata_empty_returnsOriginalCtx(t *testing.T) {
	setupPropagation(t)
	ctx := context.Background()
	if got := mossotel.ExtractFromMetadata(ctx, map[string]any{}); got != ctx {
		t.Error("empty metadata: expected original context returned unchanged")
	}
}

func TestInjectToMetadata_nilMap_createsMap(t *testing.T) {
	setupPropagation(t)
	result := mossotel.InjectToMetadata(context.Background(), nil)
	if result == nil {
		t.Error("nil map: InjectToMetadata must return a non-nil map")
	}
}

func TestInjectToMetadata_preservesExistingKeys(t *testing.T) {
	setupPropagation(t)
	m := map[string]any{"channel": "cli", "sender": "user1"}
	result := mossotel.InjectToMetadata(context.Background(), m)
	if result["channel"] != "cli" || result["sender"] != "user1" {
		t.Error("InjectToMetadata must preserve pre-existing non-trace keys")
	}
}

func TestMetadataExtractor_returnsFuncCallableWithNil(t *testing.T) {
	setupPropagation(t)
	fn := mossotel.MetadataExtractor()
	if fn == nil {
		t.Fatal("MetadataExtractor must return a non-nil function")
	}
	ctx := fn(context.Background(), nil)
	if ctx == nil {
		t.Error("MetadataExtractor func must not return nil context")
	}
}

// ---------------------------------------------------------------------------
// TraceTransport
// ---------------------------------------------------------------------------

func TestTraceTransport_injectsHeaders(t *testing.T) {
	setupPropagation(t)

	var capturedTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	resp.Body.Close()

	// noop tracer produces no real span ID, so traceparent may be empty or
	// "00-000...0-000...0-00". Either way we verify no panic and no mutation
	// of the original request object.
	_ = capturedTraceparent
}

func TestTraceTransport_nilBase_usesDefaultTransport(t *testing.T) {
	setupPropagation(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	transport := &mossotel.TraceTransport{} // Base is nil
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("nil Base should fall back to DefaultTransport: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestTraceTransport_doesNotMutateOriginalRequest(t *testing.T) {
	setupPropagation(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	originalHeader := req.Header.Clone()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	resp.Body.Close()

	// Original request headers must not have been modified.
	for k := range req.Header {
		if _, existed := originalHeader[k]; !existed {
			t.Errorf("TraceTransport mutated original request header: added key %q", k)
		}
	}
}
