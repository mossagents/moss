package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
	"github.com/openai/openai-go"
)

// ─── toOpenAIMessages ────────────────────────────────

func TestToOpenAIMessages_SystemMessage(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("You are a helpful assistant.")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hello")}},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].OfSystem == nil {
		t.Fatal("expected system message")
	}
	if result[1].OfUser == nil {
		t.Fatal("expected user message")
	}
}

func TestToOpenAIMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("What's the weather?")}},
		{
			Role:         port.RoleAssistant,
			ContentParts: []port.ContentPart{port.TextPart("Let me check.")},
			ToolCalls: []port.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Beijing"}`)},
			},
		},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	asst := result[1].OfAssistant
	if asst == nil {
		t.Fatal("expected assistant message")
	}
	if len(asst.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(asst.ToolCalls))
	}
	if asst.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", asst.ToolCalls[0].Function.Name)
	}
	if asst.ToolCalls[0].Function.Arguments != `{"city":"Beijing"}` {
		t.Errorf("tool args = %q", asst.ToolCalls[0].Function.Arguments)
	}
}

func TestToOpenAIMessages_ToolResults(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleTool,
			ToolResults: []port.ToolResult{
				{CallID: "call_1", ContentParts: []port.ContentPart{port.TextPart("Sunny, 25°C")}},
				{CallID: "call_2", ContentParts: []port.ContentPart{port.TextPart("error: timeout")}, IsError: true},
			},
		},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}

	// OpenAI 要求每个 tool result 是独立的 tool message
	if len(result) != 2 {
		t.Fatalf("expected 2 tool messages, got %d", len(result))
	}
	for _, msg := range result {
		if msg.OfTool == nil {
			t.Fatal("expected tool message")
		}
	}
	if result[0].OfTool.ToolCallID != "call_1" {
		t.Errorf("tool_call_id = %q, want call_1", result[0].OfTool.ToolCallID)
	}
	raw, err := json.Marshal(result[0].OfTool)
	if err != nil {
		t.Fatalf("marshal tool message: %v", err)
	}
	if !strings.Contains(string(raw), `"content":"Sunny, 25°C"`) {
		t.Fatalf("expected tool message content to marshal as string, got %s", string(raw))
	}
}

func TestToOpenAIMessages_EmptyAssistantSkipped(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("")}, ToolCalls: nil},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}

	if len(result) != 0 {
		t.Fatalf("expected 0 messages for empty assistant, got %d", len(result))
	}
}

func TestToOpenAIMessages_UserWithInputImage(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleUser,
			ContentParts: []port.ContentPart{
				port.TextPart("describe this"),
				port.ImageInlinePart(port.ContentPartInputImage, "image/png", "abcd", "a.png"),
			},
		},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}
	raw, err := json.Marshal(result[0].OfUser)
	if err != nil {
		t.Fatalf("marshal user message: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"type":"image_url"`) {
		t.Fatalf("expected image_url content part, got %s", payload)
	}
	if !strings.Contains(payload, `"url":"data:image/png;base64,abcd"`) {
		t.Fatalf("expected data URL image source, got %s", payload)
	}
}

func TestToOpenAIMessages_UserWithInputAudio(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleUser,
			ContentParts: []port.ContentPart{
				port.TextPart("transcribe this"),
				port.MediaInlinePart(port.ContentPartInputAudio, "audio/wav", "abcd", "a.wav"),
			},
		},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}
	raw, err := json.Marshal(result[0].OfUser)
	if err != nil {
		t.Fatalf("marshal user message: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"type":"input_audio"`) {
		t.Fatalf("expected input_audio content part, got %s", payload)
	}
	if !strings.Contains(payload, `"format":"wav"`) {
		t.Fatalf("expected wav format, got %s", payload)
	}
}

func TestToOpenAIMessages_UserWithInputVideo(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleUser,
			ContentParts: []port.ContentPart{
				port.TextPart("summarize this video"),
				port.MediaInlinePart(port.ContentPartInputVideo, "video/mp4", "abcd", "clip.mp4"),
			},
		},
	}
	result, err := toOpenAIMessages(msgs, "gpt-4o")
	if err != nil {
		t.Fatalf("toOpenAIMessages: %v", err)
	}
	raw, err := json.Marshal(result[0].OfUser)
	if err != nil {
		t.Fatalf("marshal user message: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"type":"file"`) {
		t.Fatalf("expected file content part, got %s", payload)
	}
	if !strings.Contains(payload, `"filename":"clip.mp4"`) {
		t.Fatalf("expected clip.mp4 filename, got %s", payload)
	}
}

func TestToOpenAIMessages_UnsupportedPartFails(t *testing.T) {
	msgs := []port.Message{
		{
			Role:         port.RoleUser,
			ContentParts: []port.ContentPart{{Type: port.ContentPartOutputImage, URL: "https://example.com/out.png"}},
		},
	}
	_, err := toOpenAIMessages(msgs, "gpt-4o")
	if err == nil {
		t.Fatal("expected unsupported part error")
	}
}

// ─── toOpenAITools ───────────────────────────────────

func TestToOpenAITools(t *testing.T) {
	tools := []port.ToolSpec{
		{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Name:        "ls",
			Description: "",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
	result := toOpenAITools(tools)

	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Function.Name != "read_file" {
		t.Errorf("tool name = %q, want read_file", result[0].Function.Name)
	}
}

func TestToOpenAITools_Empty(t *testing.T) {
	result := toOpenAITools(nil)
	if result != nil {
		t.Errorf("expected nil for empty tools, got %v", result)
	}
}

// ─── fromOpenAIResponse ──────────────────────────────

func TestFromOpenAIResponse_TextOnly(t *testing.T) {
	raw := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello World"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromOpenAIResponse(&completion)

	if resp.Message.Role != port.RoleAssistant {
		t.Errorf("role = %s, want assistant", resp.Message.Role)
	}
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "Hello World" {
		t.Errorf("content = %q, want Hello World", got)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.StopReason != "stop" {
		t.Errorf("stop_reason = %q, want stop", resp.StopReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens = %d, want 5", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestFromOpenAIResponse_WithToolCalls(t *testing.T) {
	raw := `{
		"id": "chatcmpl-456",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Let me check.",
				"tool_calls": [{
					"id": "call_abc",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"city\":\"Beijing\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {
			"prompt_tokens": 20,
			"completion_tokens": 15,
			"total_tokens": 35
		}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromOpenAIResponse(&completion)

	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "Let me check." {
		t.Errorf("content = %q", got)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call id = %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("tool call name = %q", tc.Name)
	}
	if string(tc.Arguments) != `{"city":"Beijing"}` {
		t.Errorf("tool call args = %s", tc.Arguments)
	}
	if resp.StopReason != "tool_calls" {
		t.Errorf("stop_reason = %q, want tool_calls", resp.StopReason)
	}
}

func TestFromOpenAIResponse_WithAudio(t *testing.T) {
	raw := `{
		"id": "chatcmpl-456a",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Here is audio",
				"audio": {
					"id": "aud_1",
					"data": "YWJjZA==",
					"expires_at": 1700000100,
					"transcript": "abc"
				}
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 20,
			"completion_tokens": 15,
			"total_tokens": 35
		}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := fromOpenAIResponse(&completion)
	if len(resp.Message.ContentParts) != 2 {
		t.Fatalf("expected text+audio content parts, got %d", len(resp.Message.ContentParts))
	}
	if resp.Message.ContentParts[1].Type != port.ContentPartOutputAudio {
		t.Fatalf("expected output_audio part, got %s", resp.Message.ContentParts[1].Type)
	}
}

func TestFromOpenAIResponse_WithReasoningContent(t *testing.T) {
	raw := `{
		"id": "chatcmpl-r1",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "deepseek-reasoner",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_content": "Need to verify the redirect first.",
				"content": "The site redirects with HTTP 301."
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromOpenAIResponse(&completion)
	if len(resp.Message.ContentParts) != 2 {
		t.Fatalf("expected reasoning+text parts, got %d", len(resp.Message.ContentParts))
	}
	if resp.Message.ContentParts[0].Type != port.ContentPartReasoning {
		t.Fatalf("expected first part reasoning, got %s", resp.Message.ContentParts[0].Type)
	}
	if got := port.ContentPartsToReasoningText(resp.Message.ContentParts); got != "Need to verify the redirect first." {
		t.Fatalf("reasoning = %q", got)
	}
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "The site redirects with HTTP 301." {
		t.Fatalf("plain text = %q", got)
	}
}

func TestFromOpenAIResponse_ReasoningOnly(t *testing.T) {
	raw := `{
		"id": "chatcmpl-r2",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "deepseek-reasoner",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_content": "This requires more analysis."
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromOpenAIResponse(&completion)
	if len(resp.Message.ContentParts) != 1 {
		t.Fatalf("expected reasoning-only part, got %d", len(resp.Message.ContentParts))
	}
	if resp.Message.ContentParts[0].Type != port.ContentPartReasoning {
		t.Fatalf("expected reasoning part, got %s", resp.Message.ContentParts[0].Type)
	}
	if got := port.ContentPartsToReasoningText(resp.Message.ContentParts); got != "This requires more analysis." {
		t.Fatalf("reasoning = %q", got)
	}
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "" {
		t.Fatalf("plain text = %q, want empty", got)
	}
}

func TestFromOpenAIResponse_EmptyChoices(t *testing.T) {
	raw := `{
		"id": "chatcmpl-789",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "gpt-4o",
		"choices": [],
		"usage": {"prompt_tokens": 5, "completion_tokens": 0, "total_tokens": 5}
	}`
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromOpenAIResponse(&completion)
	if resp.Message.Role != port.RoleAssistant {
		t.Errorf("role = %s, want assistant", resp.Message.Role)
	}
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "" {
		t.Errorf("expected empty content, got %q", got)
	}
}

// ─── streamIterator (processChunk / flushToolCalls) ──

func newTestIterator() *streamIterator {
	return &streamIterator{
		toolBuilders: make(map[int]*toolCallBuilder),
	}
}

func chunkFromJSON(t *testing.T, raw string) openai.ChatCompletionChunk {
	t.Helper()
	var c openai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	return c
}

func TestStreamIterator_TextDeltas(t *testing.T) {
	it := newTestIterator()

	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"content":" World"},"finish_reason":""}]
	}`))

	if len(it.pending) != 2 {
		t.Fatalf("expected 2 pending chunks, got %d", len(it.pending))
	}
	if it.pending[0].Delta != "Hello" {
		t.Errorf("chunk[0].Delta = %q, want Hello", it.pending[0].Delta)
	}
	if it.pending[1].Delta != " World" {
		t.Errorf("chunk[1].Delta = %q, want ' World'", it.pending[1].Delta)
	}
}

func TestStreamIterator_ReasoningDeltas(t *testing.T) {
	it := newTestIterator()

	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-r1","object":"chat.completion.chunk","created":1,"model":"deepseek-reasoner",
		"choices":[{"index":0,"delta":{"reasoning_content":"First inspect the redirect chain. "},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-r1","object":"chat.completion.chunk","created":1,"model":"deepseek-reasoner",
		"choices":[{"index":0,"delta":{"reasoning_content":"Then call the weather API."},"finish_reason":""}]
	}`))

	if len(it.pending) != 2 {
		t.Fatalf("expected 2 pending chunks, got %d", len(it.pending))
	}
	if it.pending[0].ReasoningDelta != "First inspect the redirect chain." {
		t.Fatalf("reasoning delta[0] = %q", it.pending[0].ReasoningDelta)
	}
	if it.pending[1].ReasoningDelta != "Then call the weather API." {
		t.Fatalf("reasoning delta[1] = %q", it.pending[1].ReasoningDelta)
	}
}

func TestStreamIterator_SingleToolCall(t *testing.T) {
	it := newTestIterator()

	// 第一个 chunk：工具调用开始
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-2","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":""}]
	}`))
	// 参数增量
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-2","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-2","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":""}]
	}`))
	// finish_reason = tool_calls → 触发 flush
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-2","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]
	}`))

	// pending 中不应有文本 delta，只有工具调用
	var toolChunks []port.StreamChunk
	for _, p := range it.pending {
		if p.ToolCall != nil {
			toolChunks = append(toolChunks, p)
		}
	}
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 tool call chunk, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	if tc.ID != "call_abc" {
		t.Errorf("tool call ID = %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("tool call Name = %q", tc.Name)
	}
	if string(tc.Arguments) != `{"city":"Beijing"}` {
		t.Errorf("tool call Arguments = %s", tc.Arguments)
	}
}

func TestStreamIterator_ParallelToolCalls(t *testing.T) {
	it := newTestIterator()

	// 两个并行工具调用
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-3","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[
			{"index":0,"id":"call_1","type":"function","function":{"name":"func_a","arguments":""}},
			{"index":1,"id":"call_2","type":"function","function":{"name":"func_b","arguments":""}}
		]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-3","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[
			{"index":0,"function":{"arguments":"{\"a\":1}"}},
			{"index":1,"function":{"arguments":"{\"b\":2}"}}
		]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-3","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]
	}`))

	var toolChunks []port.StreamChunk
	for _, p := range it.pending {
		if p.ToolCall != nil {
			toolChunks = append(toolChunks, p)
		}
	}
	if len(toolChunks) != 2 {
		t.Fatalf("expected 2 tool call chunks, got %d", len(toolChunks))
	}

	names := map[string]string{}
	for _, tc := range toolChunks {
		names[tc.ToolCall.Name] = string(tc.ToolCall.Arguments)
	}
	if names["func_a"] != `{"a":1}` {
		t.Errorf("func_a args = %q", names["func_a"])
	}
	if names["func_b"] != `{"b":2}` {
		t.Errorf("func_b args = %q", names["func_b"])
	}
}

func TestStreamIterator_EmptyToolArgs(t *testing.T) {
	it := newTestIterator()

	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-4","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"no_args","arguments":""}}]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-4","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]
	}`))

	var tc *port.ToolCall
	for _, p := range it.pending {
		if p.ToolCall != nil {
			tc = p.ToolCall
		}
	}
	if tc == nil {
		t.Fatal("expected tool call")
	}
	// 空参数应该被替换为 "{}"
	if string(tc.Arguments) != "{}" {
		t.Errorf("expected empty args as {}, got %s", tc.Arguments)
	}
}

func TestStreamIterator_TruncatedEscapedToolArgs_Repaired(t *testing.T) {
	it := newTestIterator()

	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-8","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_z","type":"function","function":{"name":"echo","arguments":"{\"path\":\"C:\\\\Users\\\\foo\\\""}}]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-8","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]
	}`))

	var tc *port.ToolCall
	for _, p := range it.pending {
		if p.ToolCall != nil {
			tc = p.ToolCall
		}
	}
	if tc == nil {
		t.Fatal("expected tool call")
	}
	if !json.Valid(tc.Arguments) {
		t.Fatalf("expected repaired valid JSON args, got %s", tc.Arguments)
	}
}

func TestStreamIterator_UsageTracking(t *testing.T) {
	it := newTestIterator()

	// 中间 chunk 无 usage
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-5","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}]
	}`))

	if it.usage.TotalTokens != 0 {
		t.Errorf("expected 0 total tokens before final chunk, got %d", it.usage.TotalTokens)
	}

	// 最后一个 chunk 带 usage
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-5","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[],
		"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}
	}`))

	if it.usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", it.usage.PromptTokens)
	}
	if it.usage.CompletionTokens != 3 {
		t.Errorf("completion_tokens = %d, want 3", it.usage.CompletionTokens)
	}
	if it.usage.TotalTokens != 13 {
		t.Errorf("total_tokens = %d, want 13", it.usage.TotalTokens)
	}
}

func TestStreamIterator_EmptyChoicesIgnored(t *testing.T) {
	it := newTestIterator()

	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-6","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[]
	}`))

	if len(it.pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(it.pending))
	}
}

func TestStreamIterator_TextAndToolCallMixed(t *testing.T) {
	it := newTestIterator()

	// 先输出文本
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-7","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"content":"Let me check."},"finish_reason":""}]
	}`))
	// 然后工具调用
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-7","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_m","type":"function","function":{"name":"search","arguments":"{\"q\":\"test\"}"}}]},"finish_reason":""}]
	}`))
	it.processChunk(chunkFromJSON(t, `{
		"id":"cc-7","object":"chat.completion.chunk","created":1,"model":"gpt-4o",
		"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]
	}`))

	// 应该先有文本 delta，再有工具调用
	if len(it.pending) != 2 {
		t.Fatalf("expected 2 pending chunks, got %d", len(it.pending))
	}
	if it.pending[0].Delta != "Let me check." {
		t.Errorf("first chunk should be text delta, got %+v", it.pending[0])
	}
	if it.pending[1].ToolCall == nil {
		t.Error("second chunk should be tool call")
	}
}
