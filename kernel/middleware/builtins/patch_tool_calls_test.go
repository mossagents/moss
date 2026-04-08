package builtins

import (
	"context"
	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"testing"
)

func TestPatchToolCalls_BackfillsMissingResults(t *testing.T) {
	sess := &session.Session{
		ID: "s1",
		Messages: []mdl.Message{
			{
				Role: mdl.RoleAssistant,
				ToolCalls: []mdl.ToolCall{
					{ID: "call_1", Name: "grep"},
					{ID: "call_2", Name: "read_file"},
				},
			},
			{
				Role: mdl.RoleTool,
				ToolResults: []mdl.ToolResult{
					{CallID: "call_1", ContentParts: []mdl.ContentPart{mdl.TextPart("ok")}},
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
	if last.Role != mdl.RoleTool || len(last.ToolResults) != 1 {
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
		Messages: []mdl.Message{
			{
				Role:      mdl.RoleAssistant,
				ToolCalls: []mdl.ToolCall{{ID: "call_1", Name: "grep"}},
			},
			{
				Role: mdl.RoleTool,
				ToolResults: []mdl.ToolResult{
					{CallID: "call_1", ContentParts: []mdl.ContentPart{mdl.TextPart("ok")}},
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
