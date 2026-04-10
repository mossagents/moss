package budget

import "sync"

// Threshold defines a named budget usage threshold with a callback.
type Threshold struct {
	Name      string  // e.g., "warning", "critical"
	Percent   float64 // 0.0-1.0, e.g., 0.8 for 80%
	OnReached func(snapshot BudgetSnapshot)
}

// ThresholdMonitor watches budget usage and fires callbacks when thresholds are crossed.
type ThresholdMonitor struct {
	mu         sync.Mutex
	thresholds []Threshold
	fired      map[string]bool
}

// NewThresholdMonitor creates a ThresholdMonitor with the given thresholds.
func NewThresholdMonitor(thresholds ...Threshold) *ThresholdMonitor {
	return &ThresholdMonitor{
		thresholds: thresholds,
		fired:      make(map[string]bool),
	}
}

// Check evaluates the snapshot against all thresholds, firing any not yet triggered.
func (m *ThresholdMonitor) Check(snapshot BudgetSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, th := range m.thresholds {
		if m.fired[th.Name] {
			continue
		}
		if snapshot.TokensPct() >= th.Percent || snapshot.StepsPct() >= th.Percent {
			m.fired[th.Name] = true
			if th.OnReached != nil {
				th.OnReached(snapshot)
			}
		}
	}
}

// Reset clears the fired state so thresholds can fire again.
func (m *ThresholdMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fired = make(map[string]bool)
}
