package observe

import (
	"fmt"
	"sort"
	"strings"
)

// ReleaseGate represents a single release gate criterion.
type ReleaseGate struct {
	Name        string
	Description string
	Threshold   float64
	MetricKey   string
	// RequiredMetricKey, when set, must exist and be > 0 before MetricKey is
	// considered valid for gate evaluation. This prevents averages/rates from
	// vacuously passing on empty snapshots.
	RequiredMetricKey string
	Operator    string // "gte" (>=), "lte" (<=)
	Enabled     bool
}

// GateResult captures the outcome of a single gate check.
type GateResult struct {
	Gate   ReleaseGate
	Value  float64
	Passed bool
	Reason string
}

// GateStatus aggregates all gate results for a release snapshot.
type GateStatus struct {
	Timestamp   string
	Environment string
	Results     []GateResult
	AllPassed   bool
	FailCount   int
}

// ReleaseGateMeter validates metrics snapshots against release gate thresholds.
type ReleaseGateMeter struct {
	gates map[string]ReleaseGate
}

// NewReleaseGateMeter creates a gate meter with production default thresholds.
func NewReleaseGateMeter() *ReleaseGateMeter {
	return NewReleaseGateMeterForEnvironment("prod")
}

// NewReleaseGateMeterForEnvironment creates a gate meter with environment-
// specific default thresholds.
func NewReleaseGateMeterForEnvironment(env string) *ReleaseGateMeter {
	return &ReleaseGateMeter{
		gates: releaseGatesForEnvironment(env),
	}
}

// NormalizeReleaseGateEnvironment normalizes release gate environments to the
// supported prod/staging/dev profiles.
func NormalizeReleaseGateEnvironment(env string) string {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "staging":
		return "staging"
	case "dev":
		return "dev"
	default:
		return "prod"
	}
}

func releaseGatesForEnvironment(env string) map[string]ReleaseGate {
	gates := map[string]ReleaseGate{
		"success_rate": {
			Name:        "success_rate",
			Description: "Run success rate (completed / total)",
			Threshold:   0.95,
			MetricKey:   "success.rate",
			Operator:    "gte",
			Enabled:     true,
		},
		"llm_latency_avg": {
			Name:        "llm_latency_avg",
			Description: "Average LLM latency (ms)",
			Threshold:   10000,
			MetricKey:   "latency.llm_avg_ms",
			RequiredMetricKey: "latency.llm_ms_count",
			Operator:    "lte",
			Enabled:     true,
		},
		"tool_latency_avg": {
			Name:        "tool_latency_avg",
			Description: "Average tool latency (ms)",
			Threshold:   5000,
			MetricKey:   "latency.tool_avg_ms",
			RequiredMetricKey: "latency.tool_ms_count",
			Operator:    "lte",
			Enabled:     true,
		},
		"tool_error_rate": {
			Name:        "tool_error_rate",
			Description: "Tool error rate (errors / total calls)",
			Threshold:   0.05,
			MetricKey:   "tool_error.rate",
			RequiredMetricKey: "tool_error.calls_total",
			Operator:    "lte",
			Enabled:     true,
		},
		"guardian_error_rate": {
			Name:        "guardian_error_rate",
			Description: "Guardian review error rate (errors / total reviews)",
			Threshold:   0.01,
			MetricKey:   "guardian.error_rate",
			Operator:    "lte",
			Enabled:     true,
		},
		"replay_prepared_total": {
			Name:        "replay_prepared_total",
			Description: "Checkpoint replay probe coverage",
			Threshold:   1,
			MetricKey:   "replay.prepared_total",
			Operator:    "gte",
			Enabled:     true,
		},
	}

	switch NormalizeReleaseGateEnvironment(env) {
	case "staging":
		gates["success_rate"] = withGateThreshold(gates["success_rate"], 0.90)
		gates["llm_latency_avg"] = withGateThreshold(gates["llm_latency_avg"], 15000)
		gates["tool_latency_avg"] = withGateThreshold(gates["tool_latency_avg"], 8000)
		gates["tool_error_rate"] = withGateThreshold(gates["tool_error_rate"], 0.10)
	case "dev":
		gates["success_rate"] = withGateThreshold(gates["success_rate"], 0.80)
		gates["llm_latency_avg"] = withGateEnabled(gates["llm_latency_avg"], false)
		gates["tool_latency_avg"] = withGateEnabled(gates["tool_latency_avg"], false)
		gates["tool_error_rate"] = withGateEnabled(gates["tool_error_rate"], false)
		gates["guardian_error_rate"] = withGateEnabled(gates["guardian_error_rate"], false)
		gates["replay_prepared_total"] = withGateEnabled(gates["replay_prepared_total"], false)
	}

	return gates
}

func withGateThreshold(gate ReleaseGate, threshold float64) ReleaseGate {
	gate.Threshold = threshold
	return gate
}

func withGateEnabled(gate ReleaseGate, enabled bool) ReleaseGate {
	gate.Enabled = enabled
	return gate
}

// SetGate updates or adds a gate configuration.
func (m *ReleaseGateMeter) SetGate(gate ReleaseGate) {
	m.gates[gate.Name] = gate
}

// ValidateSnapshot checks a metrics snapshot against all enabled gates.
func (m *ReleaseGateMeter) ValidateSnapshot(snapshot NormalizedMetricsSnapshot, env string) GateStatus {
	metricsMap := snapshot.Map()
	var results []GateResult
	failCount := 0

	names := make([]string, 0, len(m.gates))
	for name := range m.gates {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		gate := m.gates[name]
		if !gate.Enabled {
			continue
		}

		value, exists := metricsMap[gate.MetricKey]
		if !exists {
			results = append(results, GateResult{
				Gate:   gate,
				Value:  0,
				Passed: false,
				Reason: fmt.Sprintf("metric '%s' not found in snapshot", gate.MetricKey),
			})
			failCount++
			continue
		}
		if gate.RequiredMetricKey != "" {
			requiredValue, ok := metricsMap[gate.RequiredMetricKey]
			if !ok || requiredValue <= 0 {
				results = append(results, GateResult{
					Gate:   gate,
					Value:  value,
					Passed: false,
					Reason: fmt.Sprintf("metric '%s' has insufficient data (requires %s > 0)", gate.MetricKey, gate.RequiredMetricKey),
				})
				failCount++
				continue
			}
		}

		passed := m.compareValue(value, gate.Threshold, gate.Operator)
		reason := fmt.Sprintf("%s %s %f (value: %f)", gate.MetricKey, gate.Operator, gate.Threshold, value)
		if !passed {
			reason += " [FAILED]"
			failCount++
		}

		results = append(results, GateResult{
			Gate:   gate,
			Value:  value,
			Passed: passed,
			Reason: reason,
		})
	}

	return GateStatus{
		Timestamp:   "",
		Environment: NormalizeReleaseGateEnvironment(env),
		Results:     results,
		AllPassed:   failCount == 0,
		FailCount:   failCount,
	}
}

// compareValue evaluates a metric value against a threshold using the operator.
func (m *ReleaseGateMeter) compareValue(value, threshold float64, operator string) bool {
	switch operator {
	case "gte":
		return value >= threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}

// Report generates a human-readable gate validation report.
func (status GateStatus) Report() string {
	report := fmt.Sprintf("=== Release Gate Status ===\n")
	report += fmt.Sprintf("Environment: %s\n", status.Environment)
	report += fmt.Sprintf("Overall: %s (%d gate(s) failed)\n\n",
		boolToString(status.AllPassed, "✓ PASSED", "✗ FAILED"), status.FailCount)

	report += "Gate Results:\n"
	for _, result := range status.Results {
		icon := boolToString(result.Passed, "✓", "✗")
		report += fmt.Sprintf("  %s %s: %s\n", icon, result.Gate.Name, result.Reason)
	}

	return report
}

// boolToString is a helper for readability.
func boolToString(b bool, trueStr, falseStr string) string {
	if b {
		return trueStr
	}
	return falseStr
}
