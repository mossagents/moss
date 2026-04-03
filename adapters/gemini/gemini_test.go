package gemini

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/mossagents/moss/kernel/port"
)

func TestToGeminiContents_UserMultimodal(t *testing.T) {
	system, contents, err := toGeminiContents([]port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("sys")}},
		{
			Role: port.RoleUser,
			ContentParts: []port.ContentPart{
				port.TextPart("describe"),
				port.MediaInlinePart(port.ContentPartInputImage, "image/png", base64.StdEncoding.EncodeToString([]byte("img")), "a.png"),
			},
		},
	}, DefaultModel)
	if err != nil {
		t.Fatalf("toGeminiContents: %v", err)
	}
	if system == nil || len(system.Parts) != 1 || system.Parts[0].Text != "sys" {
		t.Fatalf("unexpected system content: %+v", system)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0].Role != genai.RoleUser {
		t.Fatalf("expected role user, got %s", contents[0].Role)
	}
	if len(contents[0].Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(contents[0].Parts))
	}
	if contents[0].Parts[0].Text != "describe" {
		t.Fatalf("unexpected text part: %+v", contents[0].Parts[0])
	}
	if contents[0].Parts[1].InlineData == nil {
		t.Fatalf("expected inline media part, got %+v", contents[0].Parts[1])
	}
}

func TestToGeminiContents_ToolRoundTrip(t *testing.T) {
	system, contents, err := toGeminiContents([]port.Message{
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("weather?")}},
		{
			Role: port.RoleAssistant,
			ToolCalls: []port.ToolCall{
				{ID: "c1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"beijing"}`)},
			},
		},
		{
			Role: port.RoleTool,
			ToolResults: []port.ToolResult{
				{CallID: "c1", ContentParts: []port.ContentPart{port.TextPart("sunny")}},
			},
		},
	}, DefaultModel)
	if err != nil {
		t.Fatalf("toGeminiContents: %v", err)
	}
	if system != nil {
		t.Fatalf("expected nil system")
	}
	if len(contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(contents))
	}
	modelCall := contents[1]
	if modelCall.Role != genai.RoleModel {
		t.Fatalf("expected model role for assistant tool call, got %s", modelCall.Role)
	}
	if len(modelCall.Parts) != 1 || modelCall.Parts[0].FunctionCall == nil {
		t.Fatalf("expected function call part, got %+v", modelCall.Parts)
	}
	userToolResult := contents[2]
	if len(userToolResult.Parts) != 1 || userToolResult.Parts[0].FunctionResponse == nil {
		t.Fatalf("expected function response part, got %+v", userToolResult.Parts)
	}
	if userToolResult.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Fatalf("unexpected function response name: %s", userToolResult.Parts[0].FunctionResponse.Name)
	}
}

func TestToGeminiTool(t *testing.T) {
	tool, err := toGeminiTool([]port.ToolSpec{
		{
			Name:        "read_file",
			Description: "Read file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	})
	if err != nil {
		t.Fatalf("toGeminiTool: %v", err)
	}
	if len(tool.FunctionDeclarations) != 1 {
		t.Fatalf("expected one function declaration, got %d", len(tool.FunctionDeclarations))
	}
	if tool.FunctionDeclarations[0].Name != "read_file" {
		t.Fatalf("unexpected function declaration name: %s", tool.FunctionDeclarations[0].Name)
	}
}

func TestFromGeminiResponse_WithTextAndToolCall(t *testing.T) {
	resp := fromGeminiResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				FinishReason: genai.FinishReasonStop,
				Content: &genai.Content{
					Role: genai.RoleModel,
					Parts: []*genai.Part{
						genai.NewPartFromText("hello"),
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "c1",
								Name: "get_weather",
								Args: map[string]any{"city": "beijing"},
							},
						},
					},
				},
			},
		},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	})
	if got := port.ContentPartsToPlainText(resp.Message.ContentParts); got != "hello" {
		t.Fatalf("unexpected text: %q", got)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected tool name: %s", resp.ToolCalls[0].Name)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if !strings.EqualFold(resp.StopReason, string(genai.FinishReasonStop)) {
		t.Fatalf("unexpected stop reason: %s", resp.StopReason)
	}
}

func TestStreamIterator_ProcessResponse(t *testing.T) {
	it := &streamIterator{
		seenToolCalls: make(map[string]struct{}),
	}
	it.processResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						genai.NewPartFromText("hi"),
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "c1",
								Name: "toolA",
								Args: map[string]any{"x": 1},
							},
						},
					},
				},
			},
		},
	})
	if len(it.pending) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(it.pending))
	}
	if it.pending[0].Delta != "hi" {
		t.Fatalf("unexpected first delta: %+v", it.pending[0])
	}
	if it.pending[1].ToolCall == nil || it.pending[1].ToolCall.Name != "toolA" {
		t.Fatalf("unexpected tool call chunk: %+v", it.pending[1])
	}
}
