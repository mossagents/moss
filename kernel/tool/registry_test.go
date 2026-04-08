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
