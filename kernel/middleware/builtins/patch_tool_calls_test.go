package builtins

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func TestPatchToolCalls_BackfillsMissingResults(t *testing.T) {
	sess := &session.Session{
		ID: "s1",
		Messages: []port.Message{
			{
				Role: port.RoleAssistant,
				ToolCalls: []port.ToolCall{
					{ID: "call_1", Name: "grep"},
					{ID: "call_2", Name: "read_file"},
				},
			},
			{
				Role: port.RoleTool,
				ToolResults: []port.ToolResult{
					{CallID: "call_1", Content: "ok"},
				},
			},
		},
	}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}
	mw := PatchToolCalls()
	if err := mw(context.Background(), mc, func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("PatchToolCalls: %v", err)
	}
	last := sess.Messages[len(sess.Messages)-1]
	if last.Role != port.RoleTool || len(last.ToolResults) != 1 {
		t.Fatalf("expected one patched tool message, got %+v", last)
	}
	if last.ToolResults[0].CallID != "call_2" {
		t.Fatalf("patched call_id=%q", last.ToolResults[0].CallID)
	}
	if !last.ToolResults[0].IsError {
		t.Fatal("patched result should be marked as error")
	}
}

func TestPatchToolCalls_NoPatchWhenComplete(t *testing.T) {
	sess := &session.Session{
		ID: "s2",
		Messages: []port.Message{
			{
				Role:      port.RoleAssistant,
				ToolCalls: []port.ToolCall{{ID: "call_1", Name: "grep"}},
			},
			{
				Role: port.RoleTool,
				ToolResults: []port.ToolResult{
					{CallID: "call_1", Content: "ok"},
				},
			},
		},
	}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}
	mw := PatchToolCalls()
	if err := mw(context.Background(), mc, func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("PatchToolCalls: %v", err)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("expected unchanged message count, got %d", len(sess.Messages))
	}
}
