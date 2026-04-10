package prometheus_test

import (
	"context"
	"errors"
	mossprom "github.com/mossagents/moss/contrib/telemetry/prometheus"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"strings"
	"testing"
	"time"
)

func newTestObs(t *testing.T) (*mossprom.Observer, *prom.Registry) {
	t.Helper()
	reg := prom.NewRegistry()
	obs, err := mossprom.New(reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return obs, reg
}

// sumCounter gathers all metrics from reg and sums values for the named counter.
func sumCounter(t *testing.T, reg *prom.Registry, metricName string) float64 {
	t.Helper()
	mfs := mustGather(t, reg)
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

func mustGather(t *testing.T, reg *prom.Registry) []*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	return mfs
}

func hasLabelValue(m *dto.Metric, name, value string) bool {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name && lp.GetValue() == value {
			return true
		}
	}
	return false
}

func TestObserverImplementsPortObserver(t *testing.T) {
	obs, _ := newTestObs(t)
	var _ observe.Observer = obs
}

func TestNew_doubleRegisterReturnsError(t *testing.T) {
	reg := prom.NewRegistry()
	if _, err := mossprom.New(reg); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := mossprom.New(reg); err == nil {
		t.Fatal("second New: expected error on duplicate registration, got nil")
	}
}

func TestOnLLMCall(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveLLMCall(context.Background(), obs, observe.LLMCallEvent{
		Model:            "gpt-4o",
		Duration:         300 * time.Millisecond,
		StopReason:       "end_turn",
		Usage:            model.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
		EstimatedCostUSD: 0.002,
	})

	if v := sumCounter(t, reg, "moss_llm_calls_total"); v != 1 {
		t.Errorf("moss_llm_calls_total: want 1 got %v", v)
	}
	if v := sumCounter(t, reg, "moss_llm_tokens_total"); v != 150 {
		t.Errorf("moss_llm_tokens_total: want 150 got %v", v)
	}
	if v := sumCounter(t, reg, "moss_llm_cost_usd_total"); v == 0 {
		t.Errorf("moss_llm_cost_usd_total: want > 0 got %v", v)
	}
	// histogram should have 1 observation
	if n := testutil.CollectAndCount(reg, "moss_llm_call_duration_seconds"); n != 1 {
		t.Errorf("moss_llm_call_duration_seconds series: want 1 got %d", n)
	}
}

func TestOnLLMCall_withError(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveLLMCall(context.Background(), obs, observe.LLMCallEvent{
		Model: "claude-3-5-sonnet",
		Error: errors.New("rate limit"),
	})

	// error label "true" series should exist
	mfs := mustGather(t, reg)
	found := false
	for _, mf := range mfs {
		if mf.GetName() != "moss_llm_calls_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if hasLabelValue(m, "error", "true") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected error=true label in moss_llm_calls_total")
	}
}

func TestOnToolCall(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveToolCall(context.Background(), obs, observe.ToolCallEvent{
		ToolName: "bash",
		Risk:     "high",
		Duration: 200 * time.Millisecond,
		Error:    errors.New("exit 1"),
	})

	if v := sumCounter(t, reg, "moss_tool_calls_total"); v != 1 {
		t.Errorf("moss_tool_calls_total: want 1 got %v", v)
	}
}

func TestOnSessionEvent(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveSessionEvent(context.Background(), obs, observe.SessionEvent{Type: "completed"})
	observe.ObserveSessionEvent(context.Background(), obs, observe.SessionEvent{Type: "failed"})

	if v := sumCounter(t, reg, "moss_sessions_total"); v != 2 {
		t.Errorf("moss_sessions_total: want 2 got %v", v)
	}
}

func TestOnApproval_pending(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveApproval(context.Background(), obs, io.ApprovalEvent{
		Request: io.ApprovalRequest{Kind: io.ApprovalKindTool},
	})

	mfs := mustGather(t, reg)
	hasPending := false
	for _, mf := range mfs {
		if mf.GetName() != "moss_approvals_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if hasLabelValue(m, "decision", "pending") {
				hasPending = true
			}
		}
	}
	if !hasPending {
		t.Fatal("decision label: expected pending series")
	}
}

func TestOnApproval_approved(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveApproval(context.Background(), obs, io.ApprovalEvent{
		Request:  io.ApprovalRequest{Kind: io.ApprovalKindTool},
		Decision: &io.ApprovalDecision{Approved: true},
	})

	if v := sumCounter(t, reg, "moss_approvals_total"); v != 1 {
		t.Errorf("moss_approvals_total: want 1 got %v", v)
	}
}

func TestOnError(t *testing.T) {
	obs, reg := newTestObs(t)
	observe.ObserveError(context.Background(), obs, observe.ErrorEvent{Phase: "loop"})

	if v := sumCounter(t, reg, "moss_errors_total"); v != 1 {
		t.Errorf("moss_errors_total: want 1 got %v", v)
	}
}

func TestMetricDescriptions(t *testing.T) {
	obs, reg := newTestObs(t)
	// Pre-seed one observation per metric family; CounterVec only appears in
	// Gather() output after at least one label combination has been recorded.
	ctx := context.Background()
	observe.ObserveLLMCall(ctx, obs, observe.LLMCallEvent{
		Model: "m", StopReason: "end_turn",
		Usage:            model.TokenUsage{PromptTokens: 1, CompletionTokens: 1},
		EstimatedCostUSD: 0.001,
	})
	observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{ToolName: "t", Risk: "low"})
	observe.ObserveSessionEvent(ctx, obs, observe.SessionEvent{Type: "created"})
	observe.ObserveApproval(ctx, obs, io.ApprovalEvent{Request: io.ApprovalRequest{Kind: io.ApprovalKindTool}})
	observe.ObserveError(ctx, obs, observe.ErrorEvent{Phase: "p"})

	mfs := mustGather(t, reg)
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	want := []string{
		"moss_approvals_total",
		"moss_errors_total",
		"moss_llm_call_duration_seconds",
		"moss_llm_calls_total",
		"moss_llm_cost_usd_total",
		"moss_llm_tokens_total",
		"moss_sessions_total",
		"moss_tool_call_duration_seconds",
		"moss_tool_calls_total",
	}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected metric %q to be registered; got: %s", w, strings.Join(names, ", "))
		}
	}
}

func TestObserver_NormalizedMetricsMap(t *testing.T) {
	obs, _ := newTestObs(t)
	ctx := context.Background()
	observe.ObserveLLMCall(ctx, obs, observe.LLMCallEvent{Duration: 100 * time.Millisecond, EstimatedCostUSD: 0.01})
	observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{ToolName: "read_file", Duration: 20 * time.Millisecond})
	observe.ObserveToolCall(ctx, obs, observe.ToolCallEvent{ToolName: "run_command", Duration: 30 * time.Millisecond, Error: errors.New("fail")})
	observe.ObserveSessionEvent(ctx, obs, observe.SessionEvent{Type: "completed"})

	m := obs.NormalizedMetricsMap()
	if m["success.run_total"] != 1 {
		t.Fatalf("run total = %v", m["success.run_total"])
	}
	if m["tool_error.calls_total"] != 2 || m["tool_error.errors_total"] != 1 {
		t.Fatalf("tool error counters mismatch: %+v", m)
	}
}

