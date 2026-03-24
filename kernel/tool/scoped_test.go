package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScopedRegistry(t *testing.T) {
	parent := NewRegistry()
	parent.Register(ToolSpec{Name: "read_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})
	parent.Register(ToolSpec{Name: "write_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})
	parent.Register(ToolSpec{Name: "run_command"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})

	scoped := Scoped(parent, []string{"read_file", "write_file"})

	// List only sees allowed tools
	list := scoped.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d tools, want 2", len(list))
	}

	// Get allowed tool works
	_, _, ok := scoped.Get("read_file")
	if !ok {
		t.Error("read_file should be accessible")
	}

	// Get disallowed tool fails
	_, _, ok = scoped.Get("run_command")
	if ok {
		t.Error("run_command should not be accessible in scoped registry")
	}
}
