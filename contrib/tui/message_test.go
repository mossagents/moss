package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSICodes(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

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
	for _, want := range []string{"│ tool · Bash · go test ./...", "command", "go test ./..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderMessage(tool start) missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "risk=high") || strings.Contains(out, "id=call-1") || strings.Contains(out, "(shell)") {
		t.Fatalf("crush-style shell start should hide verbose metadata, got %q", out)
	}
}

func TestRenderMessage_UserUsesLeadingDotWithoutLegacyLabel(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgUser,
		content: "hello world",
	}, 80)
	if !strings.Contains(out, "●") || !strings.Contains(out, "hello world") {
		t.Fatalf("user message should use leading dot format: %q", out)
	}
	if strings.Contains(out, "You") {
		t.Fatalf("user message should not show legacy label: %q", out)
	}
}

func TestRenderMessage_AssistantUsesLeadingDotWithoutLegacyLabel(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgAssistant,
		content: "hi there",
	}, 80)
	if !strings.Contains(out, "●") || !strings.Contains(out, "hi there") {
		t.Fatalf("assistant message should use leading dot format: %q", out)
	}
	if strings.Contains(out, "moss") {
		t.Fatalf("assistant message should not show legacy label: %q", out)
	}
}

func TestRenderMessage_PlainChineseDialogueAlignsWithEventSummary(t *testing.T) {
	user := stripANSICodes(renderMessage(chatMessage{
		kind:    msgUser,
		content: "你好",
	}, 80))
	assistant := stripANSICodes(renderMessage(chatMessage{
		kind:    msgAssistant,
		content: "你好，我来帮你。",
	}, 80))
	reasoning := stripANSICodes(renderMessage(chatMessage{
		kind:    msgReasoning,
		content: "先看一下当前上下文。",
	}, 80))

	if strings.Contains(user, "●  你好") {
		t.Fatalf("user dialogue should not include extra paragraph indent: %q", user)
	}
	if strings.Contains(assistant, "●  你好") {
		t.Fatalf("assistant dialogue should not include extra paragraph indent: %q", assistant)
	}
	userColumn := strings.Index(user, "你好")
	assistantColumn := strings.Index(assistant, "你好")
	reasoningColumn := strings.Index(reasoning, "thinking")
	if userColumn != reasoningColumn {
		t.Fatalf("user content column = %d, want %d; user=%q reasoning=%q", userColumn, reasoningColumn, user, reasoning)
	}
	if assistantColumn != reasoningColumn {
		t.Fatalf("assistant content column = %d, want %d; assistant=%q reasoning=%q", assistantColumn, reasoningColumn, assistant, reasoning)
	}
}

func TestRenderMessage_AssistantMediaUsesMediaHint(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgAssistant,
		content: "Generated audio",
		meta: map[string]any{
			"is_media":   true,
			"media_kind": "audio",
			"media_path": "out.wav",
		},
	}, 80)
	if !strings.Contains(out, "Generated audio: out.wav") || !strings.Contains(out, "/media open") {
		t.Fatalf("assistant media output missing expected hint: %q", out)
	}
}

func TestRenderMessage_ProgressShowsThinkingDetail(t *testing.T) {
	ts := time.Date(2026, 4, 4, 12, 34, 56, 0, time.UTC)
	// thinking 阶段不再添加到 transcript，改用 tools 阶段测试 progress 渲染
	out := renderMessage(chatMessage{
		kind:    msgProgress,
		content: "running run_command",
		meta: map[string]any{
			"phase":     "tools",
			"timestamp": ts,
		},
	}, 80)
	for _, want := range []string{"│ progress", "using tools", "running run_command"} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress message missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "...") {
		t.Fatalf("progress message should no longer use legacy ellipsis style: %q", out)
	}
	if stamp := formatMessageTimestamp(map[string]any{"timestamp": ts}); stamp != "" && strings.Contains(out, stamp) {
		t.Fatalf("progress message should not show timestamp: %q", out)
	}
}

func TestCompactRunSummary_ShowsBudgetExhaustedStatus(t *testing.T) {
	got := compactRunSummary("Run summary: | status=budget_exhausted | steps=2 | tokens=124075 | cost=n/a")
	if !strings.Contains(got, "budget exhausted") {
		t.Fatalf("expected budget exhausted status in %q", got)
	}
	if strings.Contains(got, "completed") {
		t.Fatalf("did not expect completed status in %q", got)
	}
}

func TestRenderMessage_TimestampNotShownForAnyRole(t *testing.T) {
	ts := time.Date(2026, 4, 4, 12, 34, 56, 0, time.UTC)
	stamp := formatMessageTimestamp(map[string]any{"timestamp": ts})
	assistant := renderMessage(chatMessage{
		kind:    msgAssistant,
		content: "done",
		meta:    map[string]any{"timestamp": ts},
	}, 80)
	if strings.Contains(assistant, stamp) {
		t.Fatalf("assistant message should not show timestamp: %q", assistant)
	}

	user := renderMessage(chatMessage{
		kind:    msgUser,
		content: "run it",
		meta:    map[string]any{"timestamp": ts},
	}, 80)
	if strings.Contains(user, stamp) {
		t.Fatalf("user message should not show timestamp: %q", user)
	}
}

func TestRenderMessage_ReasoningShowsTranscriptBlock(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgReasoning,
		content: "First inspect the redirect chain.\nThen query the API.",
	}, 80)
	for _, want := range []string{"│ thinking", "First inspect the redirect chain. Then query the API."} {
		if !strings.Contains(out, want) {
			t.Fatalf("reasoning message missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("reasoning message should stay on one line: %q", out)
	}
}

func TestRenderMessage_ReasoningWrapsWhenTooLong(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgReasoning,
		content: "First inspect the redirect chain and then query the weather API with the normalized Hangzhou location before summarizing the result.",
	}, 44)
	if !strings.Contains(out, "\n") {
		t.Fatalf("reasoning message should wrap when too long: %q", out)
	}
	for _, want := range []string{"│ thinking", "First inspect", "chain and then query"} {
		if !strings.Contains(out, want) {
			t.Fatalf("reasoning message missing %q in %q", want, out)
		}
	}
}

func TestRenderMessage_ReasoningWrapsChineseSafely(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgReasoning,
		content: "我获取到了杭州的天气数据，这是一个JSON格式的详细天气信息。让我分析一下数据并提供总结。",
	}, 28)
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected wrapped reasoning output, got %q", out)
	}
	if strings.Contains(out, "�") {
		t.Fatalf("reasoning output should not contain broken unicode: %q", out)
	}
	for _, want := range []string{"│ thinking", "我获取到", "杭州的天气"} {
		if !strings.Contains(out, want) {
			t.Fatalf("reasoning output missing %q in %q", want, out)
		}
	}
}

func TestRenderAllMessages_SuppressesShortReasoningFragments(t *testing.T) {
	out := stripANSICodes(renderAllMessages([]chatMessage{
		{kind: msgReasoning, content: "Let"},
		{kind: msgToolStart, content: "grep", meta: map[string]any{"tool": "grep", "call_id": "a", "args_preview": `{"pattern":"FastMode"}`}},
		{kind: msgToolResult, content: `[{"path":"runtime_support.go"}]`, meta: map[string]any{"tool": "grep", "call_id": "a"}},
		{kind: msgReasoning, content: "Need to inspect the redirect chain first."},
	}, 96, true))
	if strings.Contains(out, "thinking · Let") || strings.Contains(out, "│ thinking  Let") {
		t.Fatalf("expected low-signal reasoning fragment to be hidden, got %q", out)
	}
	if !strings.Contains(out, "Need to inspect the redirect chain first.") {
		t.Fatalf("expected substantive reasoning to remain visible, got %q", out)
	}
}

func TestRenderAllMessages_CollapsedShowsPerToolSummary(t *testing.T) {
	out := renderAllMessages([]chatMessage{
		{kind: msgToolStart, content: "glob", meta: map[string]any{"tool": "glob", "call_id": "a", "args_preview": `{"pattern":"**/*"}`}},
		{kind: msgToolResult, content: `[{"path":"README.md"}]`, meta: map[string]any{"tool": "glob", "call_id": "a"}},
		{kind: msgToolStart, content: "view", meta: map[string]any{"tool": "view", "call_id": "b", "args_preview": `{"path":"README.md"}`}},
		{kind: msgToolResult, content: `"hello"`, meta: map[string]any{"tool": "view", "call_id": "b"}},
	}, 80, true)
	for _, want := range []string{"│ tool · Glob · **/*", "│ tool · View · README.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("collapsed per-tool summary missing %q in %q", want, out)
		}
	}
	for _, unwanted := range []string{"result", "hello", "· summary"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("collapsed per-tool summary should hide detail %q in %q", unwanted, out)
		}
	}
}

func TestRenderAllMessages_AddsBreathingRoomBetweenDialogueBlocks(t *testing.T) {
	out := renderAllMessages([]chatMessage{
		{kind: msgUser, content: "你好"},
		{kind: msgAssistant, content: "你好，我来帮你。"},
		{kind: msgProgress, content: "running http_request", meta: map[string]any{"phase": "tools"}},
		{kind: msgProgress, content: "approval required", meta: map[string]any{"phase": "approval"}},
		{kind: msgAssistant, content: "已经处理完成。"},
	}, 80, false)
	if !regexp.MustCompile(`你好\s*\n\n\s*●\s*你好，我来帮你。`).MatchString(out) {
		t.Fatalf("expected blank line between dialogue blocks, got %q", out)
	}
	if !regexp.MustCompile(`running http_request\s*\n\s*│ progress · awaiting approval`).MatchString(out) {
		t.Fatalf("expected dense event stack without extra blank line, got %q", out)
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

func TestRenderMessage_ToolResultDecodesJSONStringPayload(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: "\"line1\\r\\nline2\\r\\nline3\"",
		meta:    map[string]any{"tool": "read_file"},
	}, 80)
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("decoded tool result missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "\\r\\n") {
		t.Fatalf("expected escaped newlines to be decoded, got %q", out)
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
	for _, want := range []string{"│ tool · Read State · status: ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool result missing %q in %q", want, out)
		}
	}
	for _, unwanted := range []string{"JSON object", `"status": "ok"`, `"count": 2`} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("tool result should stay compact and hide %q in %q", unwanted, out)
		}
	}
}

func TestRenderMessage_ToolResultSummarizesJSONArray(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolResult,
		content: `[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"}]`,
		meta:    map[string]any{"tool": "list_threads"},
	}, 80)
	for _, want := range []string{"│ tool · List Threads · 4 items"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool array summary missing %q in %q", want, out)
		}
	}
	for _, unwanted := range []string{`1. {"id":"a"}`, `3. {"id":"c"}`, "... 1 more items", `4. {"id":"d"}`} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("tool array summary should stay compact and hide %q in %q", unwanted, out)
		}
	}
}

func TestRenderAllMessages_CombinesToolStartAndResult(t *testing.T) {
	out := renderAllMessages([]chatMessage{
		{
			kind:    msgToolStart,
			content: "read_file",
			meta: map[string]any{
				"tool":         "read_file",
				"call_id":      "call-1",
				"args_preview": `{"path":"README.md"}`,
			},
		},
		{
			kind:    msgToolResult,
			content: "\"line1\\nline2\"",
			meta: map[string]any{
				"tool":        "read_file",
				"call_id":     "call-1",
				"duration_ms": int64(9),
			},
		},
	}, 100, false)
	if strings.Count(out, "Read File") != 1 {
		t.Fatalf("expected combined tool item with one header, got %q", out)
	}
	for _, want := range []string{"README.md", "result", "line1", "line2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("combined tool item missing %q in %q", want, out)
		}
	}
}

func TestRenderAllMessages_ExpandsToolDetailsAcrossInterveningMessages(t *testing.T) {
	msgs := []chatMessage{
		{
			kind:    msgToolStart,
			content: "http_request",
			meta: map[string]any{
				"tool":         "http_request",
				"call_id":      "call-http",
				"args_preview": `{"url":"https://wttr.in/hangzhou?format=j1","timeout_seconds":10}`,
				"completed_at": "2026-04-04T09:00:00Z",
			},
		},
		{kind: msgSystem, content: "Approval granted."},
		{
			kind:    msgToolResult,
			content: `{"body":"{\"current_condition\":[]}","status":200}`,
			meta: map[string]any{
				"tool":        "http_request",
				"call_id":     "call-http",
				"duration_ms": int64(174),
			},
		},
	}

	collapsed := renderAllMessages(msgs, 100, true)
	if strings.Contains(collapsed, "result") || strings.Contains(collapsed, `"body":`) {
		t.Fatalf("collapsed tool view should hide detail body, got %q", collapsed)
	}
	if !strings.Contains(collapsed, "│ tool · Http Request · https://wttr.in/hangzhou?format=j1 · status: 200") {
		t.Fatalf("collapsed tool view missing compact summary: %q", collapsed)
	}

	expanded := renderAllMessages(msgs, 100, false)
	for _, want := range []string{"result · status: 200", "Approval granted."} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded tool view missing %q in %q", want, expanded)
		}
	}
	for _, unwanted := range []string{`"status": 200`, `"body":`} {
		if strings.Contains(expanded, unwanted) {
			t.Fatalf("expanded tool view should hide verbose detail %q in %q", unwanted, expanded)
		}
	}
	if strings.Count(expanded, "Http Request") != 1 {
		t.Fatalf("expected completed tool to render once at result position, got %q", expanded)
	}
}

func TestRenderMessage_ToolErrorUsesErrorHeader(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolError,
		content: "permission denied",
		meta:    map[string]any{"tool": "run_command", "duration_ms": int64(12)},
	}, 80)
	for _, want := range []string{"│ error · Bash", "12ms", "permission denied"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tool error missing %q in %q", want, out)
		}
	}
}

func TestRenderMessage_ShellToolStartUsesCompactLayout(t *testing.T) {
	out := renderMessage(chatMessage{
		kind:    msgToolStart,
		content: "run_command",
		meta: map[string]any{
			"tool": "run_command",
			"args_preview": `{
				"description":"Commit and push C4 integration updates",
				"command":"git --no-pager status --short",
				"args":["&&","git","add","runtime/memory.go","&&","git","push","origin","main"]
			}`,
		},
	}, 100)
	for _, want := range []string{
		"│ tool · Bash · git --no-pager status --short",
		"task",
		"Commit and push C4 integration updates",
		"command",
		"git --no-pager status --short && git add runtime/memory.go",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("compact shell start missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "risk=") || strings.Contains(out, "args") {
		t.Fatalf("compact shell start should hide verbose metadata, got %q", out)
	}
}

func TestRenderMessage_ShellToolResultUsesCompactLayout(t *testing.T) {
	out := renderMessage(chatMessage{
		kind: msgToolResult,
		content: `{
			"exit_code": 0,
			"stdout": "2026-04-03 Friday\\r\\n",
			"stderr": ""
		}`,
		meta: map[string]any{
			"tool":        "run_command",
			"duration_ms": int64(180),
		},
	}, 100)
	for _, want := range []string{"│ tool · Bash", "180ms", "exit=0", "stdout: 2026-04-03 Friday"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compact shell result missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "JSON object") || strings.Contains(out, "\"exit_code\"") {
		t.Fatalf("compact shell result should hide verbose json block, got %q", out)
	}
}
