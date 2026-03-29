package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewJinaReaderRequestEscapesNestedURL(t *testing.T) {
	req, err := newJinaReaderRequest(context.Background(), jinaReaderParams{
		URL:            "https://example.com/article?id=123&lang=en",
		TargetSelector: "main",
		RemoveSelector: ".ads",
		TokenBudget:    2048,
	})
	if err != nil {
		t.Fatalf("newJinaReaderRequest: %v", err)
	}
	if got, want := req.URL.String(), "https://r.jina.ai/https:%2F%2Fexample.com%2Farticle%3Fid=123&lang=en"; got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
	if req.Header.Get("X-Target-Selector") != "main" {
		t.Fatal("missing target selector header")
	}
	if req.Header.Get("X-Remove-Selector") != ".ads" {
		t.Fatal("missing remove selector header")
	}
	if req.Header.Get("X-Token-Budget") != "2048" {
		t.Fatal("missing token budget header")
	}
}

func TestNewJinaSearchRequestUsesContextAndQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := newJinaSearchRequest(ctx, jinaSearchParams{
		Query: "agent memory",
		Count: 5,
		GL:    "us",
		HL:    "en",
	})
	if err != nil {
		t.Fatalf("newJinaSearchRequest: %v", err)
	}
	if req.Context() != ctx {
		t.Fatal("request did not keep provided context")
	}
	if got, want := req.URL.Query().Get("count"), "5"; got != want {
		t.Fatalf("count query = %q, want %q", got, want)
	}
	if got, want := req.URL.Query().Get("gl"), "us"; got != want {
		t.Fatalf("gl query = %q, want %q", got, want)
	}
	if got, want := req.URL.Query().Get("hl"), "en"; got != want {
		t.Fatalf("hl query = %q, want %q", got, want)
	}
}

func TestCompactJinaPayloadReaderTruncatesLongContent(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"title":   "Gold market update",
			"content": strings.Repeat("A", 4000),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	compacted, err := unwrapJinaPayload(body, "reader")
	if err != nil {
		t.Fatalf("unwrap payload: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(compacted, &out); err != nil {
		t.Fatalf("unmarshal compacted payload: %v", err)
	}
	content, _ := out["content"].(string)
	if len(content) == 0 {
		t.Fatal("expected compacted content")
	}
	if !strings.Contains(content, "[truncated]") {
		t.Fatalf("expected truncated marker, got %q", content)
	}
	if len([]rune(content)) > jinaStringLimit("reader", "content")+20 {
		t.Fatalf("content too long after truncation: %d", len([]rune(content)))
	}
}

func TestCompactJinaPayloadSearchLimitsResults(t *testing.T) {
	results := make([]any, 0, 6)
	for i := 0; i < 6; i++ {
		results = append(results, map[string]any{
			"title":   "item",
			"content": strings.Repeat("B", 1200),
		})
	}
	body, err := json.Marshal(map[string]any{"data": results})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	compacted, err := unwrapJinaPayload(body, "search")
	if err != nil {
		t.Fatalf("unwrap payload: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(compacted, &out); err != nil {
		t.Fatalf("unmarshal compacted payload: %v", err)
	}
	resultsOut, ok := out["results"].([]any)
	if !ok {
		t.Fatalf("expected search results array, got %#v", out["results"])
	}
	if len(resultsOut) != 4 {
		t.Fatalf("expected 4 results, got %d", len(resultsOut))
	}
	if _, ok := out["retrieved_at"].(string); !ok {
		t.Fatalf("expected retrieved_at metadata, got %#v", out["retrieved_at"])
	}
}

func TestCompactJinaPayloadSearchParsesNestedJSONString(t *testing.T) {
	inner, err := json.Marshal([]map[string]any{
		{"title": "今日金价 (2026年3月25日)", "content": "stale page"},
	})
	if err != nil {
		t.Fatalf("marshal inner payload: %v", err)
	}
	body, err := json.Marshal(map[string]any{"data": string(inner)})
	if err != nil {
		t.Fatalf("marshal outer payload: %v", err)
	}
	compacted, err := unwrapJinaPayload(body, "search")
	if err != nil {
		t.Fatalf("unwrap payload: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(compacted, &out); err != nil {
		t.Fatalf("unmarshal compacted payload: %v", err)
	}
	resultsOut, ok := out["results"].([]any)
	if !ok || len(resultsOut) != 1 {
		t.Fatalf("expected parsed nested results, got %#v", out["results"])
	}
}
