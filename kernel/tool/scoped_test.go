package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScopedRegistry(t *testing.T) {
	parent := NewRegistry()
	if err := parent.Register(NewRawTool(ToolSpec{Name: "read_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := parent.Register(NewRawTool(ToolSpec{Name: "write_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	if err := parent.Register(NewRawTool(ToolSpec{Name: "run_command"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register run_command: %v", err)
	}

	scoped := Scoped(parent, []string{"read_file", "write_file"})

	// List only sees allowed tools
	list := scoped.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d tools, want 2", len(list))
	}

	// Get allowed tool works
	_, ok := scoped.Get("read_file")
	if !ok {
		t.Error("read_file should be accessible")
	}

	// Get disallowed tool fails
	_, ok = scoped.Get("run_command")
	if ok {
		t.Error("run_command should not be accessible in scoped registry")
	}

	// Register should be blocked for read-only scoped view.
	err := scoped.Register(NewRawTool(ToolSpec{Name: "new_tool"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}))
	if err == nil {
		t.Fatal("expected Register to fail on scoped registry")
	}

	// Unregister should be blocked for read-only scoped view.
	err = scoped.Unregister("read_file")
	if err == nil {
		t.Fatal("expected Unregister to fail on scoped registry")
	}

	// Parent registry should remain unchanged.
	if _, ok := parent.Get("read_file"); !ok {
		t.Fatal("parent registry was unexpectedly modified")
	}
}
