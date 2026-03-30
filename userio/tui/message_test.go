package tui

import (
	"strings"
	"testing"
)

func TestRenderMessage_ToolStartIncludesArgsAndRisk(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolStart,
		content: "run_command",
		meta: map[string]any{
			"tool":         "run_command",
			"risk":         "high",
			"call_id":      "call-1",
			"args_preview": `{"command":"go test ./..."}`,
		},
	}, 80)
	for _, want := range []string{"run_command", "risk=high", "id=call-1", `args: {"command":"go test ./..."}`} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderMessage(tool start) missing %q in %q", want, out)
		}
	}
}

func TestRenderAllMessages_CollapsedCountsToolCalls(t *testing.T) {
	out := renderAllMessages([]chatMessage{
		{kind: msgToolStart, content: "glob"},
		{kind: msgToolResult, content: "ok"},
		{kind: msgToolStart, content: "view"},
		{kind: msgToolResult, content: "ok"},
	}, 80, true)
	if !strings.Contains(out, "2 tool calls") {
		t.Fatalf("collapsed summary = %q, want call count", out)
	}
}
