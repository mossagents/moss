package budget_test

import (
	"context"
	"github.com/mossagents/moss/kernel/budget"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/session"
	"testing"
)

func TestGovernor_CheckAndRecord(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 100, MaxSteps: 5}, nil)

	if !gov.Check() {
		t.Fatal("expected budget available initially")
	}

	gov.Record("s1", 50, 2)
	snap := gov.Snapshot()
	if snap.UsedTokens != 50 || snap.UsedSteps != 2 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	gov.Record("s1", 60, 3)
	if gov.Check() {
		t.Fatal("expected budget exhausted after exceeding max tokens")
	}
}

func TestGovernor_NoLimits(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	gov.Record("s1", 1_000_000, 1_000_000)
	if !gov.Check() {
		t.Fatal("expected no limit when MaxTokens/MaxSteps are 0")
	}
}

func TestGovernor_StepsLimit(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxSteps: 3}, nil)
	gov.Record("s1", 0, 3)
	if gov.Check() {
		t.Fatal("expected exhausted after 3 steps")
	}
}

func TestGovernor_Reset(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10}, nil)
	gov.Record("s1", 10, 1)
	gov.Reset()
	if !gov.Check() {
		t.Fatal("expected available after reset")
	}
}

func TestGovernor_WarnCallback(t *testing.T) {
	warned := false
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 100, WarnAt: 0.8}, func(snap budget.BudgetSnapshot) {
		warned = true
	})
	gov.Record("s1", 85, 0) // 85% > 80%
	if !warned {
		t.Fatal("expected warn callback to be invoked")
	}
}

func TestGovernor_Pct(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 200, MaxSteps: 10}, nil)
	gov.Record("s1", 100, 5)
	snap := gov.Snapshot()
	if snap.TokensPct() != 0.5 {
		t.Fatalf("expected 0.5 tokens pct, got %f", snap.TokensPct())
	}
	if snap.StepsPct() != 0.5 {
		t.Fatalf("expected 0.5 steps pct, got %f", snap.StepsPct())
	}
}

func TestBudgetGuard_Blocks(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 1}, nil)
	gov.Record("s1", 1, 0) // exhaust

	mw := budget.BudgetGuard(gov)
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "s1"},
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected ErrBudgetExhausted")
	}
}

func TestBudgetGuard_Passes(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 1000}, nil)
	mw := budget.BudgetGuard(gov)
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "s1"},
	}
	called := false
	err := mw(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("expected pass, err=%v called=%v", err, called)
	}
}

func TestBudgetRecorder_Records(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxSteps: 10}, nil)
	mw := budget.BudgetRecorder(gov)
	mc := &middleware.Context{
		Phase:   middleware.AfterLLM,
		Session: &session.Session{ID: "s1"},
	}
	_ = mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if gov.Snapshot().UsedSteps != 1 {
		t.Fatalf("expected 1 step recorded, got %d", gov.Snapshot().UsedSteps)
	}
	// Manually record tokens via BudgetTokenRecorder
	budget.BudgetTokenRecorder(gov, "s1", 100)
	if gov.Snapshot().UsedTokens != 100 {
		t.Fatalf("expected 100 tokens, got %d", gov.Snapshot().UsedTokens)
	}
}
