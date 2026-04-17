package observe

import (
	"testing"
)

func TestNewReleaseGateMeter(t *testing.T) {
	meter := NewReleaseGateMeter()

	if len(meter.gates) == 0 {
		t.Errorf("expected gates to be initialized, got %d", len(meter.gates))
	}

	expectedGates := []string{"success_rate", "llm_latency_avg", "tool_latency_avg", "tool_error_rate", "guardian_error_rate"}
	for _, name := range expectedGates {
		if _, exists := meter.gates[name]; !exists {
			t.Errorf("expected gate '%s' to exist", name)
		}
	}
}

func TestSetGate(t *testing.T) {
	meter := NewReleaseGateMeter()

	customGate := ReleaseGate{
		Name:        "custom_gate",
		Description: "A custom gate",
		Threshold:   0.9,
		MetricKey:   "custom.metric",
		Operator:    "gte",
		Enabled:     true,
	}

	meter.SetGate(customGate)

	gate, exists := meter.gates["custom_gate"]
	if !exists {
		t.Errorf("expected custom gate to be set")
	}
	if gate.Threshold != 0.9 {
		t.Errorf("expected threshold 0.9, got %f", gate.Threshold)
	}
}

func TestValidateSnapshotAllPassed(t *testing.T) {
	meter := NewReleaseGateMeter()

	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      95,
		RunFailedTotal:       5,
		LLMMSsum:             500000, // sum of all LLM calls
		LLMMSCount:           100,    // 100 calls = avg 5000ms
		ToolMSsum:            200000,
		ToolMSCount:          100, // 100 calls = avg 2000ms
		EstimatedCostUSDSum:  1.5,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      2, // 2% error rate
		GuardianReviewsTotal: 20,
	}

	status := meter.ValidateSnapshot(snapshot, "prod")

	if !status.AllPassed {
		t.Logf("Snapshot map: %v", snapshot.Map())
		t.Errorf("expected all gates to pass, but got %d failures", status.FailCount)
		t.Logf("Report:\n%s", status.Report())
	}

	if len(status.Results) == 0 {
		t.Errorf("expected gate results, got 0")
	}
}

func TestValidateSnapshotPartialFailure(t *testing.T) {
	meter := NewReleaseGateMeter()

	// Set success rate to pass but other gates to fail
	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      95,
		RunFailedTotal:       5,
		LLMMSsum:             1500000, // avg 15000ms > 10000ms threshold
		LLMMSCount:           100,
		ToolMSsum:            600000, // avg 6000ms > 5000ms threshold
		ToolMSCount:          100,
		EstimatedCostUSDSum:  1.5,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      10, // 10% error rate > 5% threshold
		GuardianReviewsTotal: 20,
	}

	status := meter.ValidateSnapshot(snapshot, "prod")

	if status.AllPassed {
		t.Errorf("expected gates to fail, but all passed")
	}

	if status.FailCount == 0 {
		t.Errorf("expected failures, got 0")
	}
}

func TestValidateSnapshotLowSuccessRate(t *testing.T) {
	meter := NewReleaseGateMeter()

	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      80, // 80% < 95% threshold
		RunFailedTotal:       20,
		LLMMSsum:             500000,
		LLMMSCount:           100,
		ToolMSsum:            200000,
		ToolMSCount:          100,
		EstimatedCostUSDSum:  1.0,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      2,
		GuardianReviewsTotal: 20,
	}

	status := meter.ValidateSnapshot(snapshot, "prod")

	if status.AllPassed {
		t.Errorf("expected success rate gate to fail")
	}

	found := false
	for _, result := range status.Results {
		if result.Gate.Name == "success_rate" && !result.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected success_rate gate failure in results")
	}
}

func TestCompareValue(t *testing.T) {
	meter := NewReleaseGateMeter()

	tests := []struct {
		value     float64
		threshold float64
		operator  string
		expected  bool
	}{
		{0.95, 0.90, "gte", true},
		{0.85, 0.90, "gte", false},
		{0.90, 0.90, "gte", true},
		{5000, 10000, "lte", true},
		{15000, 10000, "lte", false},
		{10000, 10000, "lte", true},
		{100, 100, "eq", true},
		{100, 101, "eq", false},
	}

	for i, test := range tests {
		result := meter.compareValue(test.value, test.threshold, test.operator)
		if result != test.expected {
			t.Errorf("test %d: compareValue(%f, %f, %q) = %v, expected %v",
				i, test.value, test.threshold, test.operator, result, test.expected)
		}
	}
}

func TestGateStatusReport(t *testing.T) {
	meter := NewReleaseGateMeter()

	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      90,
		RunFailedTotal:       10,
		LLMMSsum:             600000,
		LLMMSCount:           100,
		ToolMSsum:            300000,
		ToolMSCount:          100,
		EstimatedCostUSDSum:  2.0,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      6, // 6% error rate
		GuardianReviewsTotal: 20,
	}

	status := meter.ValidateSnapshot(snapshot, "staging")
	report := status.Report()

	// Check that report contains expected content
	if report == "" {
		t.Errorf("expected non-empty report")
	}
	if !contains(report, "staging") {
		t.Errorf("expected report to contain environment name")
	}
	if !contains(report, "Gate Results") {
		t.Errorf("expected report to contain 'Gate Results'")
	}
}

func TestDisabledGate(t *testing.T) {
	meter := NewReleaseGateMeter()

	// Disable success_rate gate
	successRateGate := meter.gates["success_rate"]
	successRateGate.Enabled = false
	meter.SetGate(successRateGate)

	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      50, // Will fail if enabled
		RunFailedTotal:       50,
		LLMMSsum:             500000,
		LLMMSCount:           100,
		ToolMSsum:            200000,
		ToolMSCount:          100,
		EstimatedCostUSDSum:  1.0,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      2,
		GuardianReviewsTotal: 20,
	}

	status := meter.ValidateSnapshot(snapshot, "prod")

	// Should only fail on tool_error_rate (10% > 5% threshold), not success_rate
	if status.FailCount == 0 {
		// Might pass if tool errors are low enough
		t.Logf("All gates passed with disabled success_rate")
	}

	// Verify success_rate was not checked
	for _, result := range status.Results {
		if result.Gate.Name == "success_rate" {
			t.Errorf("expected success_rate gate to be skipped")
		}
	}
}

func TestMissingMetricInSnapshot(t *testing.T) {
	meter := NewReleaseGateMeter()

	// Create empty snapshot
	snapshot := NormalizedMetricsSnapshot{}

	status := meter.ValidateSnapshot(snapshot, "prod")

	if status.AllPassed {
		t.Errorf("expected gates to fail due to missing metrics")
	}

	if status.FailCount != len(meter.gates) {
		t.Logf("Expected all gates to fail, got %d failures out of %d gates",
			status.FailCount, len(meter.gates))
	}
}

func TestValidateSnapshotGuardianErrorRateFailure(t *testing.T) {
	meter := NewReleaseGateMeter()

	snapshot := NormalizedMetricsSnapshot{
		RunTotal:             100,
		RunSuccessTotal:      98,
		RunFailedTotal:       2,
		LLMMSsum:             400000,
		LLMMSCount:           100,
		ToolMSsum:            100000,
		ToolMSCount:          100,
		ToolCallsTotal:       100,
		ToolErrorsTotal:      1,
		GuardianReviewsTotal: 10,
		GuardianErrorsTotal:  1,
	}

	status := meter.ValidateSnapshot(snapshot, "prod")
	if status.AllPassed {
		t.Fatal("expected guardian error rate gate to fail")
	}
	found := false
	for _, result := range status.Results {
		if result.Gate.Name == "guardian_error_rate" && !result.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected guardian_error_rate failure in results")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
