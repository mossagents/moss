package metrics_test

import (
	"context"
	"github.com/mossagents/moss/kernel/metrics"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"strings"
	"testing"
	"time"
)

func TestMemoryCollector_Counter(t *testing.T) {
	c := metrics.NewMemoryCollector()
	cnt := c.Counter("my_counter")
	cnt.Inc()
	cnt.Inc()
	cnt.Add(3)
	if cnt.Value() != 5 {
		t.Errorf("expected 5, got %f", cnt.Value())
	}

	// Same name should return same counter
	cnt2 := c.Counter("my_counter")
	cnt2.Inc()
	if cnt.Value() != 6 {
		t.Errorf("expected 6 after second Inc, got %f", cnt.Value())
	}
}

func TestMemoryCollector_Histogram(t *testing.T) {
	c := metrics.NewMemoryCollector()
	h := c.Histogram("latency", 0.1, 0.5, 1.0)
	h.Observe(0.05)
	h.Observe(0.3)
	h.Observe(0.8)
	h.Observe(2.0)

	snap := h.Snapshot()
	if snap.Count != 4 {
		t.Errorf("expected count=4, got %d", snap.Count)
	}
	// bucket ≤0.1 should have 1 observation (0.05)
	if snap.Buckets[0].Count != 1 {
		t.Errorf("expected bucket[0.1]=1, got %d", snap.Buckets[0].Count)
	}
	// bucket ≤0.5 should have 2 (0.05, 0.3)
	if snap.Buckets[1].Count != 2 {
		t.Errorf("expected bucket[0.5]=2, got %d", snap.Buckets[1].Count)
	}
}

func TestMemoryCollector_ExportPromText(t *testing.T) {
	c := metrics.NewMemoryCollector()
	c.Counter("req_total").Add(42)
	c.Histogram("req_duration_seconds").Observe(0.1)

	text := c.ExportPromText()
	if !strings.Contains(text, "# TYPE req_total counter") {
		t.Error("missing counter TYPE comment")
	}
	if !strings.Contains(text, "req_total 42") {
		t.Error("missing counter value")
	}
	if !strings.Contains(text, "# TYPE req_duration_seconds histogram") {
		t.Error("missing histogram TYPE comment")
	}
	if !strings.Contains(text, "req_duration_seconds_count") {
		t.Error("missing histogram count")
	}
}

func TestMetricsObserver_LLMCall(t *testing.T) {
	c := metrics.NewMemoryCollector()
	observer := metrics.NewObserver(c)

	ctx := context.Background()
	e := kobs.LLMCallEvent{
		SessionID: "s1",
		Model:     "gpt-4",
		StartedAt: time.Now(),
		Duration:  500 * time.Millisecond,
		Usage:     mdl.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
	}
	observer.OnLLMCall(ctx, e)

	llmCalls := c.Counter(metrics.MetricLLMCallsTotal)
	if llmCalls.Value() != 1 {
		t.Errorf("expected llm_calls=1, got %f", llmCalls.Value())
	}
	llmTokens := c.Counter(metrics.MetricLLMTokensTotal)
	if llmTokens.Value() != 150 {
		t.Errorf("expected llm_tokens=150, got %f", llmTokens.Value())
	}
}

func TestMetricsObserver_ToolCall(t *testing.T) {
	c := metrics.NewMemoryCollector()
	observer := metrics.NewObserver(c)

	ctx := context.Background()
	e := kobs.ToolCallEvent{
		SessionID: "s1",
		ToolName:  "bash",
		Duration:  200 * time.Millisecond,
	}
	observer.OnToolCall(ctx, e)

	toolCalls := c.Counter(metrics.MetricToolCallsTotal)
	if toolCalls.Value() != 1 {
		t.Errorf("expected tool_calls=1, got %f", toolCalls.Value())
	}
}
