package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/mossagents/moss/kernel/port"
)

// ─── toAnthropicMessages ─────────────────────────────

func TestToAnthropicMessages_SystemExtracted(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("You are a helpful assistant.")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hello")}},
	}
	system, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(system) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(system))
	}
	if system[0].Text != "You are a helpful assistant." {
		t.Errorf("system text = %q", system[0].Text)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected user role, got %s", messages[0].Role)
	}
}

func TestToAnthropicMessages_MultipleSystemMerged(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("System A")}},
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("System B")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hi")}},
	}
	system, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(system) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(system))
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestToAnthropicMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("What's the weather?")}},
		{
			Role:    port.RoleAssistant,
			ContentParts: []port.ContentPart{port.TextPart("Let me check.")},
			ToolCalls: []port.ToolCall{
				{ID: "tc_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Beijing"}`)},
			},
		},
	}
	_, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	assistantMsg := messages[1]
	if assistantMsg.Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("expected assistant role, got %s", assistantMsg.Role)
	}
	// Should have 2 content blocks: text + tool_use
	if len(assistantMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(assistantMsg.Content))
	}
}

func TestToAnthropicMessages_ToolResults(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleTool,
			ToolResults: []port.ToolResult{
				{CallID: "tc_1", ContentParts: []port.ContentPart{port.TextPart("Sunny, 25°C")}},
				{CallID: "tc_2", ContentParts: []port.ContentPart{port.TextPart("error: timeout")}, IsError: true},
			},
		},
	}
	_, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	// Tool results are sent as user message with tool_result blocks
	if messages[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected user role for tool results, got %s", messages[0].Role)
	}
	if len(messages[0].Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(messages[0].Content))
	}
}

func TestToAnthropicMessages_EmptyAssistantSkipped(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("")}, ToolCalls: nil},
	}
	_, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(messages) != 0 {
		t.Fatalf("expected 0 messages for empty assistant, got %d", len(messages))
	}
}

func TestToAnthropicMessages_UserWithInputImage(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleUser,
			ContentParts: []port.ContentPart{
				port.TextPart("describe this"),
				port.ImageInlinePart(port.ContentPartInputImage, "image/png", "abcd", "a.png"),
			},
		},
	}
	_, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}
	raw, err := json.Marshal(messages[0])
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"type":"image"`) {
		t.Fatalf("expected image block, got %s", payload)
	}
	if !strings.Contains(payload, `"type":"base64"`) {
		t.Fatalf("expected base64 image source, got %s", payload)
	}
}

func TestToAnthropicMessages_ToolResultWithOutputImage(t *testing.T) {
	msgs := []port.Message{
		{
			Role: port.RoleTool,
			ToolResults: []port.ToolResult{
				{
					CallID: "tc_1",
					ContentParts: []port.ContentPart{
						port.TextPart("done"),
						port.ImageURLPart(port.ContentPartOutputImage, "https://example.com/out.png", ""),
					},
				},
			},
		},
	}
	_, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}
	raw, err := json.Marshal(messages[0])
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"tool_result"`) || !strings.Contains(payload, `"image"`) {
		t.Fatalf("expected tool_result image content, got %s", payload)
	}
}

func TestToAnthropicMessages_UnsupportedPartFails(t *testing.T) {
	msgs := []port.Message{
		{
			Role:         port.RoleUser,
			ContentParts: []port.ContentPart{{Type: port.ContentPartOutputImage, URL: "https://example.com/out.png"}},
		},
	}
	_, _, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err == nil {
		t.Fatal("expected unsupported part error")
	}
}

// ─── toAnthropicTools ────────────────────────────────

func TestToAnthropicTools(t *testing.T) {
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
	result := toAnthropicTools(tools)

	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].OfTool == nil {
		t.Fatal("expected OfTool to be set")
	}
	if result[0].OfTool.Name != "read_file" {
		t.Errorf("tool name = %q, want read_file", result[0].OfTool.Name)
	}
}

func TestBuildParams_ResponseFormatJSONObject(t *testing.T) {
	c := New("")
	params, err := c.buildParams(port.CompletionRequest{
		Messages:       []port.Message{{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("hi")}}},
		ResponseFormat: &port.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"output_config"`) {
		t.Fatalf("expected output_config in payload: %s", text)
	}
	if !strings.Contains(text, `"schema":{"type":"object"}`) {
		t.Fatalf("expected object schema in payload: %s", text)
	}
}

func TestBuildParams_ResponseFormatJSONSchema(t *testing.T) {
	c := New("")
	params, err := c.buildParams(port.CompletionRequest{
		Messages: []port.Message{{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("hi")}}},
		ResponseFormat: &port.ResponseFormat{
			Type: "json_schema",
			JSONSchema: &port.JSONSchemaSpec{
				Name:   "trade_signal",
				Schema: json.RawMessage(`{"type":"object","properties":{"signal":{"type":"string"}},"required":["signal"]}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"signal"`) {
		t.Fatalf("expected custom schema in payload: %s", text)
	}
}

// ─── fromAnthropicResponse ───────────────────────────

func TestFromAnthropicResponse_TextOnly(t *testing.T) {
	// Construct a Message by unmarshaling JSON
	raw := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello World"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromAnthropicResponse(&msg)

	if resp.Message.Role != port.RoleAssistant {
		t.Errorf("role = %s, want assistant", resp.Message.Role)
	}
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "Hello World" {
		t.Errorf("content = %q, want Hello World", got)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", resp.StopReason)
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

func TestFromAnthropicResponse_WithToolUse(t *testing.T) {
	raw := `{
		"id": "msg_456",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "Beijing"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"stop_sequence": null,
		"usage": {"input_tokens": 20, "output_tokens": 15}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromAnthropicResponse(&msg)

	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "Let me check." {
		t.Errorf("content = %q", got)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_1" {
		t.Errorf("tool call id = %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("tool call name = %q", tc.Name)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", resp.StopReason)
	}

	// Verify arguments are valid JSON
	var args map[string]any
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args["city"] != "Beijing" {
		t.Errorf("tool call args city = %v", args["city"])
	}
}

func TestFromAnthropicResponse_MultipleToolCalls(t *testing.T) {
	raw := `{
		"id": "msg_789",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_a", "name": "read_file", "input": {"path": "/a.txt"}},
			{"type": "tool_use", "id": "toolu_b", "name": "read_file", "input": {"path": "/b.txt"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"stop_sequence": null,
		"usage": {"input_tokens": 30, "output_tokens": 20}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := fromAnthropicResponse(&msg)

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_a" {
		t.Errorf("first tool call id = %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[1].ID != "toolu_b" {
		t.Errorf("second tool call id = %q", resp.ToolCalls[1].ID)
	}
}

// ─── buildParams ─────────────────────────────────────

func TestBuildParams_Defaults(t *testing.T) {
	c := &Client{model: "test-model", maxTokens: 4096}

	req := port.CompletionRequest{
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hello")}},
		},
	}
	params, err := c.buildParams(req)
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}

	if params.Model != "test-model" {
		t.Errorf("model = %q, want test-model", params.Model)
	}
	if params.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096", params.MaxTokens)
	}
}

func TestBuildParams_ConfigOverrides(t *testing.T) {
	c := &Client{model: "default-model", maxTokens: 4096}

	req := port.CompletionRequest{
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hello")}},
		},
		Config: port.ModelConfig{
			Model:     "override-model",
			MaxTokens: 2048,
		},
	}
	params, err := c.buildParams(req)
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}

	if params.Model != "override-model" {
		t.Errorf("model = %q, want override-model", params.Model)
	}
	if params.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048", params.MaxTokens)
	}
}

func TestBuildParams_SystemSeparated(t *testing.T) {
	c := &Client{model: "m", maxTokens: 100}

	req := port.CompletionRequest{
		Messages: []port.Message{
			{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("Be concise.")}},
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Hi")}},
		},
	}
	params, err := c.buildParams(req)
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}

	if len(params.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(params.System))
	}
	if params.System[0].Text != "Be concise." {
		t.Errorf("system text = %q", params.System[0].Text)
	}
	if len(params.Messages) != 1 {
		t.Fatalf("expected 1 message (user only), got %d", len(params.Messages))
	}
}

// ─── streamIterator.processEvent ─────────────────────

func TestProcessEvent_TextDelta(t *testing.T) {
	it := &streamIterator{
		toolUseBuilders: make(map[int]*toolUseBuilder),
	}

	// Simulate content_block_delta with text_delta
	raw := `{"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "Hello"}}`
	var event anthropic.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	it.processEvent(event)

	if len(it.pending) != 1 {
		t.Fatalf("expected 1 pending chunk, got %d", len(it.pending))
	}
	if it.pending[0].Delta != "Hello" {
		t.Errorf("delta = %q, want Hello", it.pending[0].Delta)
	}
}

func TestProcessEvent_ToolUseFlow(t *testing.T) {
	it := &streamIterator{
		toolUseBuilders: make(map[int]*toolUseBuilder),
	}

	// 1. content_block_start with tool_use
	startRaw := `{"type": "content_block_start", "index": 1, "content_block": {"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {}}}`
	var startEvent anthropic.MessageStreamEventUnion
	json.Unmarshal([]byte(startRaw), &startEvent)
	it.processEvent(startEvent)

	if _, ok := it.toolUseBuilders[1]; !ok {
		t.Fatal("expected tool use builder at index 1")
	}

	// 2. input_json_delta
	delta1 := `{"type": "content_block_delta", "index": 1, "delta": {"type": "input_json_delta", "partial_json": "{\"path\""}}`
	var d1 anthropic.MessageStreamEventUnion
	json.Unmarshal([]byte(delta1), &d1)
	it.processEvent(d1)

	delta2 := `{"type": "content_block_delta", "index": 1, "delta": {"type": "input_json_delta", "partial_json": ": \"/a.txt\"}"}}`
	var d2 anthropic.MessageStreamEventUnion
	json.Unmarshal([]byte(delta2), &d2)
	it.processEvent(d2)

	if it.toolUseBuilders[1].input != `{"path": "/a.txt"}` {
		t.Errorf("accumulated input = %q", it.toolUseBuilders[1].input)
	}

	// 3. content_block_stop → emit ToolCall
	stopRaw := `{"type": "content_block_stop", "index": 1}`
	var stopEvent anthropic.MessageStreamEventUnion
	json.Unmarshal([]byte(stopRaw), &stopEvent)
	it.processEvent(stopEvent)

	if len(it.pending) != 1 {
		t.Fatalf("expected 1 pending chunk, got %d", len(it.pending))
	}
	if it.pending[0].ToolCall == nil {
		t.Fatal("expected tool call chunk")
	}
	if it.pending[0].ToolCall.ID != "toolu_1" {
		t.Errorf("tool call id = %q", it.pending[0].ToolCall.ID)
	}
	if it.pending[0].ToolCall.Name != "read_file" {
		t.Errorf("tool call name = %q", it.pending[0].ToolCall.Name)
	}

	// Verify accumulated arguments
	var args map[string]any
	json.Unmarshal(it.pending[0].ToolCall.Arguments, &args)
	if args["path"] != "/a.txt" {
		t.Errorf("tool call args path = %v", args["path"])
	}

	// Builder should be cleaned up
	if _, ok := it.toolUseBuilders[1]; ok {
		t.Error("tool use builder should be deleted after stop")
	}
}

func TestProcessEvent_MessageStopEmitsDone(t *testing.T) {
	it := &streamIterator{
		toolUseBuilders: make(map[int]*toolUseBuilder),
		usage:           port.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	raw := `{"type": "message_stop"}`
	var event anthropic.MessageStreamEventUnion
	json.Unmarshal([]byte(raw), &event)
	it.processEvent(event)

	if len(it.pending) != 1 {
		t.Fatalf("expected 1 pending chunk, got %d", len(it.pending))
	}
	if !it.pending[0].Done {
		t.Error("expected Done=true")
	}
	if it.pending[0].Usage == nil {
		t.Fatal("expected usage")
	}
	if it.pending[0].Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d", it.pending[0].Usage.PromptTokens)
	}
}

// ─── 完整消息映射对称性 ──────────────────────────────

func TestMessageConversion_RoundTrip(t *testing.T) {
	// 构建一个完整的对话序列，验证映射完整性
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("You are helpful.")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("Read /etc/hosts")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("I'll read the file.")}, ToolCalls: []port.ToolCall{
			{ID: "tc_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"/etc/hosts"}`)},
		}},
		{Role: port.RoleTool, ToolResults: []port.ToolResult{
			{CallID: "tc_1", ContentParts: []port.ContentPart{port.TextPart("127.0.0.1 localhost")}},
		}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("The file contains: 127.0.0.1 localhost")}},
	}

	system, messages, err := toAnthropicMessages(msgs, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("toAnthropicMessages: %v", err)
	}

	if len(system) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(system))
	}
	// messages: user, assistant (with tool_use), user (tool_result), assistant
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}

	// Verify alternating roles: user, assistant, user, assistant
	expectedRoles := []anthropic.MessageParamRole{
		anthropic.MessageParamRoleUser,
		anthropic.MessageParamRoleAssistant,
		anthropic.MessageParamRoleUser, // tool_results sent as user
		anthropic.MessageParamRoleAssistant,
	}
	for i, role := range expectedRoles {
		if messages[i].Role != role {
			t.Errorf("message[%d] role = %s, want %s", i, messages[i].Role, role)
		}
	}
}



