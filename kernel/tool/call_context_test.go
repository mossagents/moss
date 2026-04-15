package tool_test

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestWithToolCallContext_RoundTrip(t *testing.T) {
	meta := tool.ToolCallContext{
		SessionID: "sess-1",
		ToolName:  "read_file",
		CallID:    "call-42",
	}
	ctx := tool.WithToolCallContext(context.Background(), meta)

	got, ok := tool.ToolCallContextFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.SessionID != meta.SessionID {
		t.Errorf("SessionID: expected %q, got %q", meta.SessionID, got.SessionID)
	}
	if got.ToolName != meta.ToolName {
		t.Errorf("ToolName: expected %q, got %q", meta.ToolName, got.ToolName)
	}
	if got.CallID != meta.CallID {
		t.Errorf("CallID: expected %q, got %q", meta.CallID, got.CallID)
	}
}

func TestToolCallContextFromContext_MissingKey(t *testing.T) {
	_, ok := tool.ToolCallContextFromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false for context without tool call context")
	}
}

func TestWithToolCallContext_Overwrite(t *testing.T) {
	meta1 := tool.ToolCallContext{SessionID: "s1", ToolName: "tool1"}
	meta2 := tool.ToolCallContext{SessionID: "s2", ToolName: "tool2"}

	ctx := tool.WithToolCallContext(context.Background(), meta1)
	ctx = tool.WithToolCallContext(ctx, meta2)

	got, ok := tool.ToolCallContextFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.SessionID != "s2" {
		t.Errorf("expected SessionID=s2, got %q", got.SessionID)
	}
}
