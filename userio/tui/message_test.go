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

func TestRenderMessage_SystemPreservesPlainTextLineBreaks(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgSystem,
		content: "Available commands:\n/status  Show runtime status\n/resume  Resume saved conversation",
	}, 80)
	if !strings.Contains(out, "Available commands:") ||
		!strings.Contains(out, "/status  Show runtime status") ||
		strings.Contains(out, "Available commands: /status  Show runtime status") {
		t.Fatalf("system message lost line breaks: %q", out)
	}
}

func TestRenderMessage_ToolResultPreservesPlainTextLineBreaks(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: "Saved conversations:\nsess-1\nsess-2",
		meta:    map[string]any{"tool": "list_threads"},
	}, 80)
	if !strings.Contains(out, "Saved conversations:") ||
		!strings.Contains(out, "sess-1") ||
		strings.Contains(out, "Saved conversations: sess-1") {
		t.Fatalf("tool result lost line breaks: %q", out)
	}
}
