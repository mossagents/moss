package policystate_test

import (
	"sync"
	"testing"

	"github.com/mossagents/moss/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks/builtins"
)

func TestEnsure_NilKernel(t *testing.T) {
	state := policystate.Ensure(nil)
	if state != nil {
		t.Fatal("expected nil for nil kernel")
	}
}

func TestEnsure_ValidKernel(t *testing.T) {
	k := kernel.New()
	state := policystate.Ensure(k)
	if state == nil {
		t.Fatal("expected non-nil state for valid kernel")
	}
	// Second call returns the same instance
	state2 := policystate.Ensure(k)
	if state != state2 {
		t.Fatal("expected same State instance on repeated Ensure calls")
	}
}

func TestLookup_NotFound(t *testing.T) {
	k := kernel.New()
	st, ok := policystate.Lookup(k)
	if ok || st != nil {
		t.Fatal("expected not found before Ensure")
	}
}

func TestLookup_Found(t *testing.T) {
	k := kernel.New()
	policystate.Ensure(k)
	st, ok := policystate.Lookup(k)
	if !ok || st == nil {
		t.Fatal("expected to find state after Ensure")
	}
}

func TestLookup_NilKernel(t *testing.T) {
	st, ok := policystate.Lookup(nil)
	if ok || st != nil {
		t.Fatal("expected nil result for nil kernel")
	}
}

func TestSetAndGet(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)

	payload := map[string]any{"mode": "strict"}
	summary := map[string]any{"rules": 3}
	rules := []builtins.PolicyRule{
		builtins.CommandRules(
			builtins.CommandPatternRule{Name: "ls", Match: "ls", Access: builtins.Allow},
		),
	}

	st.Set(payload, summary, rules)

	got := st.Payload()
	if got["mode"] != "strict" {
		t.Errorf("expected mode=strict, got %v", got["mode"])
	}
	sum := st.Summary()
	if sum["rules"] != 3 {
		t.Errorf("expected rules=3, got %v", sum["rules"])
	}
	compiled := st.CompiledRules()
	if len(compiled) != len(rules) {
		t.Errorf("expected %d rules, got %d", len(rules), len(compiled))
	}
}

func TestSetAndGet_NilReceiver(t *testing.T) {
	var st *policystate.State
	st.Set(map[string]any{"k": "v"}, nil, nil) // should not panic
	if p := st.Payload(); p != nil {
		t.Error("nil receiver Payload should return nil")
	}
	if s := st.Summary(); s != nil {
		t.Error("nil receiver Summary should return nil")
	}
	if r := st.CompiledRules(); r != nil {
		t.Error("nil receiver CompiledRules should return nil")
	}
}

func TestMarkToolHookInstalled(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)

	already := st.MarkToolHookInstalled()
	if already {
		t.Fatal("expected false on first MarkToolHookInstalled")
	}
	already = st.MarkToolHookInstalled()
	if !already {
		t.Fatal("expected true on second MarkToolHookInstalled")
	}
}

func TestMarkSessionHookInstalled(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)

	already := st.MarkSessionHookInstalled()
	if already {
		t.Fatal("expected false on first MarkSessionHookInstalled")
	}
	already = st.MarkSessionHookInstalled()
	if !already {
		t.Fatal("expected true on second MarkSessionHookInstalled")
	}
}

func TestMarkHookInstalled_NilReceiver(t *testing.T) {
	var st *policystate.State
	if st.MarkToolHookInstalled() {
		t.Error("nil receiver MarkToolHookInstalled should return false")
	}
	if st.MarkSessionHookInstalled() {
		t.Error("nil receiver MarkSessionHookInstalled should return false")
	}
}

func TestConcurrentAccess(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st.Set(map[string]any{"i": i}, map[string]any{"n": i}, nil)
			_ = st.Payload()
			_ = st.Summary()
			_ = st.CompiledRules()
		}(i)
	}
	wg.Wait()
}

func TestPayloadIsolation(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)
	st.Set(map[string]any{"key": "original"}, nil, nil)

	got := st.Payload()
	got["key"] = "mutated"

	got2 := st.Payload()
	if got2["key"] != "original" {
		t.Error("Payload should return a copy, mutation should not affect internal state")
	}
}

func TestCompiledRulesIsolation(t *testing.T) {
	k := kernel.New()
	st := policystate.Ensure(k)
	rules := []builtins.PolicyRule{
		builtins.CommandRules(
			builtins.CommandPatternRule{Name: "echo", Match: "echo", Access: builtins.Allow},
		),
	}
	st.Set(nil, nil, rules)

	got := st.CompiledRules()
	got = append(got, builtins.CommandRules(
		builtins.CommandPatternRule{Name: "sh", Match: "sh", Access: builtins.Deny},
	))

	got2 := st.CompiledRules()
	if len(got2) != 1 {
		t.Error("CompiledRules should return a copy, mutation should not affect internal state")
	}
}
