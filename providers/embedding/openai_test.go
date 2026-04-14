package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mossagents/moss/providers/embedding"
)

func fakeEmbeddingServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

func TestNew_Defaults(t *testing.T) {
	e := embedding.New("test-key")
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
	if e.Dimension() != embedding.DefaultDimension {
		t.Errorf("expected default dimension %d, got %d", embedding.DefaultDimension, e.Dimension())
	}
}

func TestWithModel(t *testing.T) {
	e := embedding.New("key", embedding.WithModel("text-embedding-ada-002"))
	_ = e // Just verify it doesn't panic
}

func TestWithDimension(t *testing.T) {
	e := embedding.New("key", embedding.WithDimension(768))
	if e.Dimension() != 768 {
		t.Errorf("expected dimension=768, got %d", e.Dimension())
	}
}

func TestNewWithBaseURL(t *testing.T) {
	e := embedding.NewWithBaseURL("key", "https://my-api.example.com/v1")
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
}

func TestNewWithBaseURL_EmptyURL(t *testing.T) {
	// Empty URL falls back to default
	e := embedding.NewWithBaseURL("key", "")
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
}

func TestEmbedBatch_EmptyInput(t *testing.T) {
	e := embedding.New("key")
	results, err := e.EmbedBatch(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty input, got %v", results)
	}
}

func TestEmbedBatch_Success(t *testing.T) {
	responseBody := map[string]any{
		"data": []map[string]any{
			{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
			{"index": 1, "embedding": []float64{0.4, 0.5, 0.6}},
		},
		"model": "text-embedding-3-small",
	}

	srv := fakeEmbeddingServer(t, http.StatusOK, responseBody)
	defer srv.Close()

	e := embedding.NewWithBaseURL("test-key", srv.URL)
	results, err := e.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(results[0]) != 3 {
		t.Errorf("expected 3-dim vector, got %d", len(results[0]))
	}
}

func TestEmbedBatch_APIError(t *testing.T) {
	srv := fakeEmbeddingServer(t, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	defer srv.Close()

	e := embedding.NewWithBaseURL("bad-key", srv.URL)
	_, err := e.EmbedBatch(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestEmbedBatch_CountMismatch(t *testing.T) {
	responseBody := map[string]any{
		"data": []map[string]any{
			{"index": 0, "embedding": []float64{0.1, 0.2}},
			// Only 1 result for 2 inputs
		},
	}

	srv := fakeEmbeddingServer(t, http.StatusOK, responseBody)
	defer srv.Close()

	e := embedding.NewWithBaseURL("key", srv.URL)
	_, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for result count mismatch")
	}
}

func TestEmbed_Success(t *testing.T) {
	responseBody := map[string]any{
		"data": []map[string]any{
			{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
		},
	}

	srv := fakeEmbeddingServer(t, http.StatusOK, responseBody)
	defer srv.Close()

	e := embedding.NewWithBaseURL("test-key", srv.URL)
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3-dim vector, got %d", len(vec))
	}
}

func TestWithHTTPClient(t *testing.T) {
	srv := fakeEmbeddingServer(t, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{"index": 0, "embedding": []float64{1.0}},
		},
	})
	defer srv.Close()

	customClient := &http.Client{}
	e := embedding.NewWithBaseURL("key", srv.URL, embedding.WithHTTPClient(customClient))
	_, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error with custom client: %v", err)
	}
}
