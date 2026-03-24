package domain

import (
	"testing"
	"time"
)

func TestRunDefaults(t *testing.T) {
	run := &Run{
		RunID:     "test-run-1",
		Goal:      "test goal",
		Mode:      RunModeInteractive,
		Status:    RunStatusPending,
		StartedAt: time.Now(),
	}
	if run.RunID != "test-run-1" {
		t.Errorf("expected RunID test-run-1, got %s", run.RunID)
	}
	if run.Status != RunStatusPending {
		t.Errorf("expected status pending, got %s", run.Status)
	}
}

func TestBudget(t *testing.T) {
	b := &Budget{MaxTokens: 1000, MaxSteps: 10}
	if b.MaxTokens != 1000 {
		t.Errorf("expected MaxTokens 1000, got %d", b.MaxTokens)
	}
	if b.UsedTokens != 0 {
		t.Errorf("expected UsedTokens 0, got %d", b.UsedTokens)
	}
}
