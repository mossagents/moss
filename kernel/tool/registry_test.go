package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	spec := ToolSpec{Name: "read_file", Description: "Read a file", Risk: RiskLow}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}

	if err := r.Register(spec, handler); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, h, ok := r.Get("read_file")
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Name != "read_file" {
		t.Fatalf("Name = %q, want %q", got.Name, "read_file")
	}
	result, _ := h(context.Background(), nil)
	if string(result) != `"ok"` {
		t.Fatalf("handler result = %s, want %q", result, `"ok"`)
	}
}

func TestRegistryDuplicateRegister(t *testing.T) {
	r := NewRegistry()
	spec := ToolSpec{Name: "test"}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }

	if err := r.Register(spec, handler); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(spec, handler); err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	spec := ToolSpec{Name: "test"}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }

	if err := r.Register(spec, handler); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Unregister("test"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if _, _, ok := r.Get("test"); ok {
		t.Fatal("expected not found after unregister")
	}
}

func TestRegistryUnregisterNotFound(t *testing.T) {
	r := NewRegistry()
	if err := r.Unregister("nonexistent"); err == nil {
		t.Fatal("expected error on unregister nonexistent")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := r.Register(ToolSpec{Name: "a"}, handler); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(ToolSpec{Name: "b"}, handler); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestRegistryListByCapability(t *testing.T) {
	r := NewRegistry()
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
	if err := r.Register(ToolSpec{Name: "reader", Capabilities: []string{"read"}}, handler); err != nil {
		t.Fatalf("Register reader: %v", err)
	}
	if err := r.Register(ToolSpec{Name: "writer", Capabilities: []string{"write"}}, handler); err != nil {
		t.Fatalf("Register writer: %v", err)
	}
	if err := r.Register(ToolSpec{Name: "both", Capabilities: []string{"read", "write"}}, handler); err != nil {
		t.Fatalf("Register both: %v", err)
	}

	readers := r.ListByCapability("read")
	if len(readers) != 2 {
		t.Fatalf("ListByCapability(read) len = %d, want 2", len(readers))
	}

	writers := r.ListByCapability("write")
	if len(writers) != 2 {
		t.Fatalf("ListByCapability(write) len = %d, want 2", len(writers))
	}
}

func TestToolSpecEffectiveMetadataFallbacks(t *testing.T) {
	spec := ToolSpec{
		Name:         "write_file",
		Risk:         RiskHigh,
		Capabilities: []string{"filesystem"},
	}
	if got := spec.EffectiveEffects(); len(got) != 1 || got[0] != EffectWritesWorkspace {
		t.Fatalf("EffectiveEffects() = %v", got)
	}
	if got := spec.EffectiveSideEffectClass(); got != SideEffectWorkspace {
		t.Fatalf("EffectiveSideEffectClass() = %q", got)
	}
	if got := spec.EffectiveApprovalClass(); got != ApprovalClassExplicitUser {
		t.Fatalf("EffectiveApprovalClass() = %q", got)
	}
	if got := spec.EffectivePlannerVisibility(); got != PlannerVisibilityVisible {
		t.Fatalf("EffectivePlannerVisibility() = %q", got)
	}
	if spec.IsReadOnly() {
		t.Fatal("IsReadOnly() = true, want false")
	}
}

func TestToolSpecEffectiveMetadataHonorsExplicitFields(t *testing.T) {
	spec := ToolSpec{
		Name:               "custom",
		Risk:               RiskHigh,
		Effects:            []Effect{EffectReadOnly, EffectReadOnly},
		SideEffectClass:    SideEffectMemory,
		ApprovalClass:      ApprovalClassSupervisorOnly,
		PlannerVisibility:  PlannerVisibilityHidden,
		CommutativityClass: CommutativityFullyCommutative,
	}
	if got := spec.EffectiveEffects(); len(got) != 1 || got[0] != EffectReadOnly {
		t.Fatalf("EffectiveEffects() = %v", got)
	}
	if got := spec.EffectiveSideEffectClass(); got != SideEffectMemory {
		t.Fatalf("EffectiveSideEffectClass() = %q", got)
	}
	if got := spec.EffectiveApprovalClass(); got != ApprovalClassSupervisorOnly {
		t.Fatalf("EffectiveApprovalClass() = %q", got)
	}
	if got := spec.EffectivePlannerVisibility(); got != PlannerVisibilityHidden {
		t.Fatalf("EffectivePlannerVisibility() = %q", got)
	}
	if got := spec.EffectiveCommutativityClass(); got != CommutativityFullyCommutative {
		t.Fatalf("EffectiveCommutativityClass() = %q", got)
	}
	if !spec.IsReadOnly() {
		t.Fatal("IsReadOnly() = false, want true")
	}
}

func TestEffectiveEffects_RiskTakesPriorityOverName(t *testing.T) {
	// A RiskHigh tool whose name looks read-like must NOT be downgraded to
	// EffectReadOnly via the name heuristic.
	cases := []struct {
		name         string
		risk         RiskLevel
		capabilities []string
		wantEffect   Effect
	}{
		{
			name:       "RiskHigh no caps: read_data → ExternalSideEffect",
			risk:       RiskHigh,
			wantEffect: EffectExternalSideEffect,
		},
		{
			name:         "RiskHigh filesystem: read_data → WritesWorkspace",
			risk:         RiskHigh,
			capabilities: []string{"filesystem"},
			wantEffect:   EffectWritesWorkspace,
		},
		{
			name:         "RiskMedium workspace: get_file → WritesWorkspace",
			risk:         RiskMedium,
			capabilities: []string{"workspace"},
			wantEffect:   EffectWritesWorkspace,
		},
		{
			name:         "RiskLow filesystem: read_data → ReadOnly",
			risk:         RiskLow,
			capabilities: []string{"filesystem"},
			wantEffect:   EffectReadOnly,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := ToolSpec{Name: "read_data", Risk: tc.risk, Capabilities: tc.capabilities}
			effects := spec.EffectiveEffects()
			if len(effects) != 1 || effects[0] != tc.wantEffect {
				t.Fatalf("EffectiveEffects() = %v, want [%s]", effects, tc.wantEffect)
			}
		})
	}
}
func TestRegistryList_DeterministicOrder(t *testing.T) {
r := NewRegistry()
handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) { return nil, nil }
names := []string{"zebra", "apple", "mango", "cherry"}
for _, n := range names {
if err := r.Register(ToolSpec{Name: n}, handler); err != nil {
t.Fatalf("Register %s: %v", n, err)
}
}
got := r.List()
if len(got) != len(names) {
t.Fatalf("List() len = %d, want %d", len(got), len(names))
}
for i, spec := range got {
if spec.Name != names[i] {
t.Errorf("List()[%d] = %q, want %q (insertion order)", i, spec.Name, names[i])
}
}
// Unregister middle entry and verify order is preserved.
if err := r.Unregister("mango"); err != nil {
t.Fatal(err)
}
got = r.List()
wantAfter := []string{"zebra", "apple", "cherry"}
if len(got) != len(wantAfter) {
t.Fatalf("after unregister List() len = %d, want %d", len(got), len(wantAfter))
}
for i, spec := range got {
if spec.Name != wantAfter[i] {
t.Errorf("after unregister List()[%d] = %q, want %q", i, spec.Name, wantAfter[i])
}
}
}
