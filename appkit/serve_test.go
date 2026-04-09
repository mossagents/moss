package appkit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
)

// stubLLM satisfies kernel.WithLLM for test kernels.
type stubLLM struct{}

func (stubLLM) Complete(_ context.Context, _ model.CompletionRequest) (*model.CompletionResponse, error) {
	return &model.CompletionResponse{}, nil
}

func newHealthTestKernel(t *testing.T) *kernel.Kernel {
	t.Helper()
	return kernel.New(
		kernel.WithLLM(stubLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
	)
}

// ---------------------------------------------------------------------------
// Health – no metrics
// ---------------------------------------------------------------------------

func TestHealth_DefaultStatus(t *testing.T) {
	k := newHealthTestKernel(t)
	h := Health(k)

	if h.Status != HealthStatusOK {
		t.Errorf("Status = %q, want %q", h.Status, HealthStatusOK)
	}
	if h.ActiveRuns != 0 {
		t.Errorf("ActiveRuns = %d, want 0", h.ActiveRuns)
	}
	if h.LLMLatencyAvgMs != 0 || h.ToolLatencyAvgMs != 0 {
		t.Error("expected zero latency when no metrics provided")
	}
	if h.SuccessRate != 0 || h.ToolErrorRate != 0 {
		t.Error("expected zero rates when no metrics provided")
	}
}

func TestHealth_ShuttingDown(t *testing.T) {
	k := newHealthTestKernel(t)
	_ = k.Shutdown(context.Background())

	h := Health(k)
	if h.Status != HealthStatusShuttingDown {
		t.Errorf("Status = %q, want %q", h.Status, HealthStatusShuttingDown)
	}
}

// ---------------------------------------------------------------------------
// Health – with metrics from P2-OBS-001
// ---------------------------------------------------------------------------

func TestHealth_WithMetrics(t *testing.T) {
	k := newHealthTestKernel(t)

	snap := observe.NormalizedMetricsSnapshot{
		RunTotal:            100,
		RunSuccessTotal:     97,
		RunFailedTotal:      3,
		LLMMSsum:            250000, // 100 calls × avg 2500 ms
		LLMMSCount:          100,
		ToolMSsum:           50000, // 200 calls × avg 250 ms
		ToolMSCount:         200,
		EstimatedCostUSDSum: 1.23,
		ToolCallsTotal:      200,
		ToolErrorsTotal:     4,
	}

	h := Health(k, snap)

	if h.Status != HealthStatusOK {
		t.Errorf("Status = %q, want ok", h.Status)
	}
	if h.TotalRuns != 100 {
		t.Errorf("TotalRuns = %.0f, want 100", h.TotalRuns)
	}
	if !almostEqual(h.SuccessRate, 0.97, 1e-9) {
		t.Errorf("SuccessRate = %v, want 0.97", h.SuccessRate)
	}
	if !almostEqual(h.LLMLatencyAvgMs, 2500.0, 1e-6) {
		t.Errorf("LLMLatencyAvgMs = %v, want 2500", h.LLMLatencyAvgMs)
	}
	if !almostEqual(h.ToolLatencyAvgMs, 250.0, 1e-6) {
		t.Errorf("ToolLatencyAvgMs = %v, want 250", h.ToolLatencyAvgMs)
	}
	if !almostEqual(h.ToolErrorRate, 0.02, 1e-9) {
		t.Errorf("ToolErrorRate = %v, want 0.02", h.ToolErrorRate)
	}
}

func TestHealth_WithEmptyMetrics(t *testing.T) {
	k := newHealthTestKernel(t)
	// Zero snapshot must not panic and must yield zero values.
	h := Health(k, observe.NormalizedMetricsSnapshot{})
	if h.SuccessRate != 0 {
		t.Errorf("SuccessRate = %v, want 0", h.SuccessRate)
	}
	if h.LLMLatencyAvgMs != 0 {
		t.Errorf("LLMLatencyAvgMs = %v, want 0", h.LLMLatencyAvgMs)
	}
}

// ---------------------------------------------------------------------------
// HealthJSON
// ---------------------------------------------------------------------------

func TestHealthJSON_ContainsRequiredFields(t *testing.T) {
	k := newHealthTestKernel(t)
	snap := observe.NormalizedMetricsSnapshot{
		RunTotal:        10,
		RunSuccessTotal: 9,
		LLMMSsum:        5000,
		LLMMSCount:      5,
		ToolMSsum:       1000,
		ToolMSCount:     10,
		ToolCallsTotal:  10,
		ToolErrorsTotal: 1,
	}

	out := HealthJSON(k, snap)

	for _, field := range []string{
		`"status"`,
		`"active_runs"`,
		`"llm_latency_avg_ms"`,
		`"tool_latency_avg_ms"`,
		`"success_rate"`,
		`"tool_error_rate"`,
		`"total_runs"`,
	} {
		if !strings.Contains(out, field) {
			t.Errorf("HealthJSON missing field %s; output: %s", field, out)
		}
	}
	if !strings.Contains(out, `"status":"ok"`) {
		t.Errorf("expected status ok in JSON: %s", out)
	}
}

func TestHealthJSON_IsValidJSON(t *testing.T) {
	k := newHealthTestKernel(t)
	out := HealthJSON(k)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Errorf("HealthJSON produced invalid JSON: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// HealthText
// ---------------------------------------------------------------------------

func TestHealthText_ContainsRequiredFields(t *testing.T) {
	k := newHealthTestKernel(t)
	snap := observe.NormalizedMetricsSnapshot{
		RunTotal:        50,
		RunSuccessTotal: 48,
		LLMMSsum:        10000,
		LLMMSCount:      10,
		ToolMSsum:       2000,
		ToolMSCount:     20,
		ToolCallsTotal:  20,
		ToolErrorsTotal: 2,
	}

	out := HealthText(k, snap)

	for _, part := range []string{
		"status=ok",
		"active_runs=0",
		"llm_latency_avg_ms=",
		"tool_latency_avg_ms=",
		"success_rate=",
		"tool_error_rate=",
		"total_runs=",
	} {
		if !strings.Contains(out, part) {
			t.Errorf("HealthText missing %q; output: %s", part, out)
		}
	}
}

func TestHealthText_ShuttingDown(t *testing.T) {
	k := newHealthTestKernel(t)
	_ = k.Shutdown(context.Background())
	out := HealthText(k)
	if !strings.Contains(out, "status=shutting_down") {
		t.Errorf("expected shutting_down in HealthText: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Kernel.ActiveRunCount / Kernel.IsShuttingDown
// ---------------------------------------------------------------------------

func TestKernel_ActiveRunCount_Zero(t *testing.T) {
	k := newHealthTestKernel(t)
	if n := k.ActiveRunCount(); n != 0 {
		t.Errorf("ActiveRunCount = %d, want 0 on fresh kernel", n)
	}
}

func TestKernel_IsShuttingDown_False(t *testing.T) {
	k := newHealthTestKernel(t)
	if k.IsShuttingDown() {
		t.Error("IsShuttingDown should be false before Shutdown()")
	}
}

func TestKernel_IsShuttingDown_TrueAfterShutdown(t *testing.T) {
	k := newHealthTestKernel(t)
	_ = k.Shutdown(context.Background())
	if !k.IsShuttingDown() {
		t.Error("IsShuttingDown should be true after Shutdown()")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func almostEqual(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

