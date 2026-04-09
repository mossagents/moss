package budget_test

import (
	"context"
	"github.com/mossagents/moss/kernel/budget"
	"github.com/mossagents/moss/kernel/hooks"
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

	hook := budget.BudgetGuard(gov)
	ev := &hooks.LLMEvent{
		Session: &session.Session{ID: "s1"},
	}
	err := hook(context.Background(), ev)
	if err == nil {
		t.Fatal("expected ErrBudgetExhausted")
	}
}

func TestBudgetGuard_Passes(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 1000}, nil)
	hook := budget.BudgetGuard(gov)
	ev := &hooks.LLMEvent{
		Session: &session.Session{ID: "s1"},
	}
	err := hook(context.Background(), ev)
	if err != nil {
		t.Fatalf("expected pass, err=%v", err)
	}
}

func TestBudgetRecorder_Records(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxSteps: 10}, nil)
	hook := budget.BudgetRecorder(gov)
	ev := &hooks.LLMEvent{
		Session: &session.Session{ID: "s1"},
	}
	_ = hook(context.Background(), ev)
	if gov.Snapshot().UsedSteps != 1 {
		t.Fatalf("expected 1 step recorded, got %d", gov.Snapshot().UsedSteps)
	}
	// Manually record tokens via BudgetTokenRecorder
	budget.BudgetTokenRecorder(gov, "s1", 100)
	if gov.Snapshot().UsedTokens != 100 {
		t.Fatalf("expected 100 tokens, got %d", gov.Snapshot().UsedTokens)
	}
}

func TestGovernor_TryReserve(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 100, MaxSteps: 5}, nil)
	gov.Record("s1", 80, 3)

	// 20 tokens left, 2 steps left
	if !gov.TryReserve(20, 2) {
		t.Fatal("expected TryReserve(20,2) to succeed")
	}
	if gov.TryReserve(21, 0) {
		t.Fatal("expected TryReserve(21,0) to fail — exceeds token budget")
	}
	if gov.TryReserve(0, 3) {
		t.Fatal("expected TryReserve(0,3) to fail — exceeds step budget")
	}
	// TryReserve should not mutate state
	snap := gov.Snapshot()
	if snap.UsedTokens != 80 || snap.UsedSteps != 3 {
		t.Fatalf("TryReserve must not mutate state: %+v", snap)
	}
}

func TestGovernor_TryReserveNoLimits(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	if !gov.TryReserve(1_000_000, 1_000_000) {
		t.Fatal("expected TryReserve to succeed when no limits set")
	}
}

func TestBudgetGuard_PreCheckBlocks(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxSteps: 2}, nil)
	gov.Record("s1", 0, 2) // exactly at limit

	hook := budget.BudgetGuard(gov)
	ev := &hooks.LLMEvent{
		Session: &session.Session{ID: "s1"},
	}
	err := hook(context.Background(), ev)
	if err == nil {
		t.Fatal("expected ErrBudgetExhausted from pre-check")
	}
}
