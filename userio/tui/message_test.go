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
	for _, want := range []string{"run_command", "risk=high", "id=call-1", "args", `"command": "go test ./..."`} {
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
		content: "Available commands:\n/status  Show runtime status\n/resume  Resume saved thread",
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
		content: "Saved threads:\nsess-1\nsess-2",
		meta:    map[string]any{"tool": "list_threads"},
	}, 80)
	if !strings.Contains(out, "Saved threads:") ||
		!strings.Contains(out, "sess-1") ||
		strings.Contains(out, "Saved threads: sess-1") {
		t.Fatalf("tool result lost line breaks: %q", out)
	}
}

func TestRenderMessage_ReadFilePreservesNumberedLineBreaks(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: "1. package main\n2. import \"fmt\"\n3. func main() {}",
		meta:    map[string]any{"tool": "read_file"},
	}, 80)
	if !strings.Contains(out, "1. package main") ||
		!strings.Contains(out, "2. import \"fmt\"") ||
		!strings.Contains(out, "3. func main() {}") ||
		strings.Contains(out, "1. package main 2. import \"fmt\"") {
		t.Fatalf("read_file output lost numbered line breaks: %q", out)
	}
}

func TestRenderMessage_ToolResultFormatsJSONObject(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: `{"status":"ok","count":2}`,
		meta:    map[string]any{"tool": "read_state"},
	}, 80)
	for _, want := range []string{"JSON object", `"status": "ok"`, `"count": 2`} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool result missing %q in %q", want, out)
		}
	}
}

func TestRenderMessage_ToolResultSummarizesJSONArray(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: `[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"}]`,
		meta:    map[string]any{"tool": "list_threads"},
	}, 80)
	for _, want := range []string{"JSON array · 4 items", `1. {"id":"a"}`, `3. {"id":"c"}`, "... 1 more items"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool array summary missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, `4. {"id":"d"}`) {
		t.Fatalf("tool array summary should not include full list: %q", out)
	}
}

func TestRenderMessage_ToolErrorUsesErrorHeader(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolError,
		content: "permission denied",
		meta:    map[string]any{"tool": "run_command", "duration_ms": int64(12)},
	}, 80)
	for _, want := range []string{"❌ run_command · 12ms", "permission denied"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool error missing %q in %q", want, out)
		}
	}
}
