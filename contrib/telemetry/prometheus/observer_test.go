package prometheus_test

import (
	"context"
	"errors"
	mossprom "github.com/mossagents/moss/contrib/telemetry/prometheus"
	intr "github.com/mossagents/moss/kernel/interaction"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
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

func TestObserverImplementsPortObserver(t *testing.T) {
	obs, _ := newTestObs(t)
	var _ kobs.Observer = obs
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
	kobs.OnLLMCall(context.Background(), kobs.LLMCallEvent{
		Model:            "gpt-4o",
		Duration:         300 * time.Millisecond,
		StopReason:       "end_turn",
		Usage:            mdl.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
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
	kobs.OnLLMCall(context.Background(), kobs.LLMCallEvent{
		Model: "claude-3-5-sonnet",
		Error: errors.New("rate limit"),
	})

	// error label "true" series should exist
	mfs, _ := reg.Gather()
	found := false
	for _, mf := range mfs {
		if mf.GetName() != "moss_llm_calls_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "error" && lp.GetValue() == "true" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected error=true label in moss_llm_calls_total")
	}
}

func TestOnToolCall(t *testing.T) {
	obs, reg := newTestObs(t)
	kobs.OnToolCall(context.Background(), kobs.ToolCallEvent{
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
	kobs.OnSessionEvent(context.Background(), kobs.SessionEvent{Type: "completed"})
	kobs.OnSessionEvent(context.Background(), kobs.SessionEvent{Type: "failed"})

	if v := sumCounter(t, reg, "moss_sessions_total"); v != 2 {
		t.Errorf("moss_sessions_total: want 2 got %v", v)
	}
}

func TestOnApproval_pending(t *testing.T) {
	obs, reg := newTestObs(t)
	kobs.OnApproval(context.Background(), intr.ApprovalEvent{
		Request: intr.ApprovalRequest{Kind: intr.ApprovalKindTool},
	})

	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() != "moss_approvals_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "decision" && lp.GetValue() != "pending" {
					t.Errorf("decision label: want pending got %s", lp.GetValue())
				}
			}
		}
	}
}

func TestOnApproval_approved(t *testing.T) {
	obs, reg := newTestObs(t)
	kobs.OnApproval(context.Background(), intr.ApprovalEvent{
		Request:  intr.ApprovalRequest{Kind: intr.ApprovalKindTool},
		Decision: &intr.ApprovalDecision{Approved: true},
	})

	if v := sumCounter(t, reg, "moss_approvals_total"); v != 1 {
		t.Errorf("moss_approvals_total: want 1 got %v", v)
	}
}

func TestOnError(t *testing.T) {
	obs, reg := newTestObs(t)
	kobs.OnError(context.Background(), kobs.ErrorEvent{Phase: "loop"})

	if v := sumCounter(t, reg, "moss_errors_total"); v != 1 {
		t.Errorf("moss_errors_total: want 1 got %v", v)
	}
}

func TestMetricDescriptions(t *testing.T) {
	obs, reg := newTestObs(t)
	// Pre-seed one observation per metric family; CounterVec only appears in
	// Gather() output after at least one label combination has been recorded.
	ctx := context.Background()
	kobs.OnLLMCall(ctx, kobs.LLMCallEvent{
		Model: "m", StopReason: "end_turn",
		Usage:            mdl.TokenUsage{PromptTokens: 1, CompletionTokens: 1},
		EstimatedCostUSD: 0.001,
	})
	kobs.OnToolCall(ctx, kobs.ToolCallEvent{ToolName: "t", Risk: "low"})
	kobs.OnSessionEvent(ctx, kobs.SessionEvent{Type: "created"})
	kobs.OnApproval(ctx, intr.ApprovalEvent{Request: intr.ApprovalRequest{Kind: intr.ApprovalKindTool}})
	kobs.OnError(ctx, kobs.ErrorEvent{Phase: "p"})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
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
