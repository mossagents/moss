package budget_test

import (
	"testing"

	"github.com/mossagents/moss/kernel/budget"
)

func TestBudgetPool_AllocateAndSnapshot(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000, MaxSteps: 100}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("planner", 500, 10, 2)
	pool.Allocate("executor", 1000, 20, 1)

	snaps := pool.Snapshot()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(snaps))
	}
	// Sorted by name.
	if snaps[0].Name != "executor" || snaps[1].Name != "planner" {
		t.Fatalf("unexpected sort order: %v", snaps)
	}
	if snaps[1].MaxTokens != 500 || snaps[1].MaxSteps != 10 {
		t.Fatalf("unexpected planner limits: %+v", snaps[1])
	}
}

func TestBudgetPool_AllocateUpdate(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("a", 100, 10, 1)
	pool.Allocate("a", 200, 20, 3) // update

	snaps := pool.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 allocation after update, got %d", len(snaps))
	}
	if snaps[0].MaxTokens != 200 || snaps[0].MaxSteps != 20 || snaps[0].Priority != 3 {
		t.Fatalf("unexpected after update: %+v", snaps[0])
	}
}

func TestBudgetPool_Record(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000, MaxSteps: 100}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("planner", 500, 10, 2)

	if err := pool.Record("planner", "s1", 100, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snaps := pool.Snapshot()
	if snaps[0].UsedTokens != 100 || snaps[0].UsedSteps != 2 {
		t.Fatalf("unexpected usage: %+v", snaps[0])
	}

	// Global governor should also reflect the usage.
	gSnap := gov.Snapshot()
	if gSnap.UsedTokens != 100 || gSnap.UsedSteps != 2 {
		t.Fatalf("governor not updated: %+v", gSnap)
	}
}

func TestBudgetPool_RecordNotFound(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	err := pool.Record("nonexistent", "s1", 10, 1)
	if err == nil {
		t.Fatal("expected error for missing allocation")
	}
}

func TestBudgetPool_TryReserve(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000, MaxSteps: 100}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("planner", 100, 5, 2)
	_ = pool.Record("planner", "s1", 80, 3)

	if !pool.TryReserve("planner", 20, 2) {
		t.Fatal("expected TryReserve(20,2) to succeed")
	}
	if pool.TryReserve("planner", 21, 0) {
		t.Fatal("expected TryReserve(21,0) to fail — exceeds token allocation")
	}
	if pool.TryReserve("planner", 0, 3) {
		t.Fatal("expected TryReserve(0,3) to fail — exceeds step allocation")
	}
}

func TestBudgetPool_TryReserveNotFound(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	if pool.TryReserve("nonexistent", 1, 1) {
		t.Fatal("expected TryReserve to return false for missing allocation")
	}
}

func TestBudgetPool_TryReserveNoLimits(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("unlimited", 0, 0, 1) // 0 = no limit
	if !pool.TryReserve("unlimited", 1_000_000, 1_000_000) {
		t.Fatal("expected TryReserve to succeed with no limits")
	}
}

func TestBudgetPool_Preempt(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000, MaxSteps: 100}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("low", 500, 20, 1)
	pool.Allocate("high", 200, 5, 5)

	// low has used 100/500 tokens and 5/20 steps → 400 tokens and 15 steps spare.
	_ = pool.Record("low", "s1", 100, 5)

	rt, rs := pool.Preempt("high", 300, 10)
	if rt != 300 {
		t.Fatalf("expected 300 reclaimed tokens, got %d", rt)
	}
	if rs != 10 {
		t.Fatalf("expected 10 reclaimed steps, got %d", rs)
	}

	// Verify limits shifted.
	snaps := pool.Snapshot()
	var highSnap, lowSnap budget.AllocationSnapshot
	for _, s := range snaps {
		switch s.Name {
		case "high":
			highSnap = s
		case "low":
			lowSnap = s
		}
	}
	if highSnap.MaxTokens != 500 { // 200 + 300
		t.Fatalf("expected high max tokens 500, got %d", highSnap.MaxTokens)
	}
	if lowSnap.MaxTokens != 200 { // 500 - 300
		t.Fatalf("expected low max tokens 200, got %d", lowSnap.MaxTokens)
	}
}

func TestBudgetPool_PreemptPartial(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("low", 100, 0, 1)
	pool.Allocate("high", 50, 0, 5)
	_ = pool.Record("low", "s1", 60, 0) // 40 spare

	rt, _ := pool.Preempt("high", 200, 0) // want 200 but only 40 available
	if rt != 40 {
		t.Fatalf("expected 40 reclaimed, got %d", rt)
	}
}

func TestBudgetPool_PreemptNoDonors(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("only", 100, 10, 5)
	rt, rs := pool.Preempt("only", 50, 5)
	if rt != 0 || rs != 0 {
		t.Fatalf("expected 0 reclaimed with no donors, got tokens=%d steps=%d", rt, rs)
	}
}

func TestBudgetPool_PreemptNotFound(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{}, nil)
	pool := budget.NewBudgetPool(gov)

	rt, rs := pool.Preempt("missing", 50, 5)
	if rt != 0 || rs != 0 {
		t.Fatalf("expected 0 reclaimed for missing allocation, got %d/%d", rt, rs)
	}
}

func TestBudgetPool_SnapshotPct(t *testing.T) {
	gov := budget.NewGovernor(budget.GlobalBudget{MaxTokens: 10000}, nil)
	pool := budget.NewBudgetPool(gov)

	pool.Allocate("a", 200, 10, 1)
	_ = pool.Record("a", "s1", 100, 5)

	snaps := pool.Snapshot()
	if snaps[0].TokensPct != 0.5 {
		t.Fatalf("expected 0.5 tokens pct, got %f", snaps[0].TokensPct)
	}
	if snaps[0].StepsPct != 0.5 {
		t.Fatalf("expected 0.5 steps pct, got %f", snaps[0].StepsPct)
	}
}

// --- ThresholdMonitor tests ---

func TestThresholdMonitor_FiresAtCorrectLevel(t *testing.T) {
	var warnings, criticals int
	mon := budget.NewThresholdMonitor(
		budget.Threshold{Name: "warning", Percent: 0.8, OnReached: func(_ budget.BudgetSnapshot) { warnings++ }},
		budget.Threshold{Name: "critical", Percent: 0.95, OnReached: func(_ budget.BudgetSnapshot) { criticals++ }},
	)

	// 50% — nothing fires.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 50, MaxTokens: 100})
	if warnings != 0 || criticals != 0 {
		t.Fatal("expected no alerts at 50%")
	}

	// 80% — warning fires.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 80, MaxTokens: 100})
	if warnings != 1 {
		t.Fatalf("expected 1 warning, got %d", warnings)
	}
	if criticals != 0 {
		t.Fatal("expected no critical at 80%")
	}

	// 80% again — should not fire again.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 82, MaxTokens: 100})
	if warnings != 1 {
		t.Fatalf("expected warning not to re-fire, got %d", warnings)
	}

	// 95% — critical fires, warning already fired.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 95, MaxTokens: 100})
	if criticals != 1 {
		t.Fatalf("expected 1 critical, got %d", criticals)
	}
}

func TestThresholdMonitor_Reset(t *testing.T) {
	fired := 0
	mon := budget.NewThresholdMonitor(
		budget.Threshold{Name: "w", Percent: 0.5, OnReached: func(_ budget.BudgetSnapshot) { fired++ }},
	)

	mon.Check(budget.BudgetSnapshot{UsedTokens: 60, MaxTokens: 100})
	if fired != 1 {
		t.Fatalf("expected 1 fire, got %d", fired)
	}

	mon.Reset()
	mon.Check(budget.BudgetSnapshot{UsedTokens: 60, MaxTokens: 100})
	if fired != 2 {
		t.Fatalf("expected 2 fires after reset, got %d", fired)
	}
}

func TestThresholdMonitor_StepsPct(t *testing.T) {
	fired := false
	mon := budget.NewThresholdMonitor(
		budget.Threshold{Name: "step-warn", Percent: 0.9, OnReached: func(_ budget.BudgetSnapshot) { fired = true }},
	)

	// Token pct low but step pct high — should trigger.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 10, MaxTokens: 100, UsedSteps: 9, MaxSteps: 10})
	if !fired {
		t.Fatal("expected threshold to fire based on steps pct")
	}
}

func TestThresholdMonitor_NilCallback(t *testing.T) {
	mon := budget.NewThresholdMonitor(
		budget.Threshold{Name: "no-op", Percent: 0.5, OnReached: nil},
	)
	// Should not panic.
	mon.Check(budget.BudgetSnapshot{UsedTokens: 60, MaxTokens: 100})
}
