package gemini

import (
	"encoding/base64"
	"encoding/json"
	"github.com/mossagents/moss/kernel/model"
	"google.golang.org/genai"
	"net/http"
	"strings"
	"testing"
)

// ── constructor / options ────────────────────────────────────────────────────

func TestNewClient_Options(t *testing.T) {
	c := &Client{model: DefaultModel, maxTokens: 8192}
	WithModel("gemini-pro")(c)
	WithMaxTokens(1024)(c)
	if c.model != "gemini-pro" {
		t.Fatalf("unexpected model: %q", c.model)
	}
	if c.maxTokens != 1024 {
		t.Fatalf("unexpected maxTokens: %d", c.maxTokens)
	}
}

func TestNewClient_EmptyAPIKey(t *testing.T) {
	c := New("")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientWithHTTPClient(t *testing.T) {
	c := NewWithHTTPClient("", &http.Client{}, WithModel("gemini-flash"))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.model != "gemini-flash" {
		t.Fatalf("unexpected model: %q", c.model)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	c := NewWithBaseURL("", "https://example.com/v1", WithMaxTokens(512))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.maxTokens != 512 {
		t.Fatalf("unexpected maxTokens: %d", c.maxTokens)
	}
}

// ── effectiveModel ───────────────────────────────────────────────────────────

func TestEffectiveModel_Default(t *testing.T) {
	c := &Client{model: DefaultModel}
	if got := c.effectiveModel(model.CompletionRequest{}); got != DefaultModel {
		t.Fatalf("want %q got %q", DefaultModel, got)
	}
}

func TestEffectiveModel_Override(t *testing.T) {
	c := &Client{model: DefaultModel}
	req := model.CompletionRequest{Config: model.ModelConfig{Model: "gemini-pro"}}
	if got := c.effectiveModel(req); got != "gemini-pro" {
		t.Fatalf("want %q got %q", "gemini-pro", got)
	}
}

// ── buildConfig ──────────────────────────────────────────────────────────────

func TestBuildConfig_Defaults(t *testing.T) {
	c := &Client{maxTokens: 4096}
	cfg, err := c.buildConfig(model.CompletionRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxOutputTokens != 4096 {
		t.Fatalf("unexpected max tokens: %d", cfg.MaxOutputTokens)
	}
}

func TestBuildConfig_Temperature(t *testing.T) {
	c := &Client{maxTokens: 0}
	req := model.CompletionRequest{Config: model.ModelConfig{Temperature: 0.5}}
	cfg, err := c.buildConfig(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.5 {
		t.Fatalf("unexpected temperature: %v", cfg.Temperature)
	}
	if cfg.MaxOutputTokens != 0 {
		t.Fatalf("expected zero max tokens, got %d", cfg.MaxOutputTokens)
	}
}

func TestBuildConfig_MaxTokensOverride(t *testing.T) {
	c := &Client{maxTokens: 1000}
	req := model.CompletionRequest{Config: model.ModelConfig{MaxTokens: 512}}
	cfg, err := c.buildConfig(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxOutputTokens != 512 {
		t.Fatalf("unexpected max tokens: %d", cfg.MaxOutputTokens)
	}
}

func TestBuildConfig_Tools(t *testing.T) {
	c := &Client{maxTokens: 0}
	req := model.CompletionRequest{
		Tools: []model.ToolSpec{
			{Name: "greet", Description: "greets", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	cfg, err := c.buildConfig(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cfg.Tools))
	}
}

func TestBuildConfig_ResponseFormat_JSONObject(t *testing.T) {
	c := &Client{}
	req := model.CompletionRequest{
		ResponseFormat: &model.ResponseFormat{Type: "json_object"},
	}
	cfg, err := c.buildConfig(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResponseMIMEType != "application/json" {
		t.Fatalf("unexpected mime type: %q", cfg.ResponseMIMEType)
	}
}

func TestBuildConfig_ResponseFormat_JSONSchema(t *testing.T) {
	c := &Client{}
	req := model.CompletionRequest{
		ResponseFormat: &model.ResponseFormat{
			Type: "json_schema",
			JSONSchema: &model.JSONSchemaSpec{
				Schema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
			},
		},
	}
	cfg, err := c.buildConfig(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResponseMIMEType != "application/json" {
		t.Fatalf("unexpected mime type: %q", cfg.ResponseMIMEType)
	}
	if cfg.ResponseJsonSchema == nil {
		t.Fatal("expected ResponseJsonSchema to be set")
	}
}

func TestBuildConfig_ResponseFormat_JSONSchema_Invalid(t *testing.T) {
	c := &Client{}
	req := model.CompletionRequest{
		ResponseFormat: &model.ResponseFormat{
			Type: "json_schema",
			JSONSchema: &model.JSONSchemaSpec{
				Schema: json.RawMessage(`not-json`),
			},
		},
	}
	_, err := c.buildConfig(req, nil)
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

// ── fallbackToolName ─────────────────────────────────────────────────────────

func TestFallbackToolName(t *testing.T) {
	if got := fallbackToolName(""); got != "tool_result" {
		t.Fatalf("empty: want tool_result, got %q", got)
	}
	if got := fallbackToolName("  "); got != "tool_result" {
		t.Fatalf("whitespace: want tool_result, got %q", got)
	}
	if got := fallbackToolName("abc"); got != "abc" {
		t.Fatalf("non-empty: want abc, got %q", got)
	}
	if got := fallbackToolName("  abc  "); got != "abc" {
		t.Fatalf("trimmed: want abc, got %q", got)
	}
}

// ── inferMIMEFromURL ─────────────────────────────────────────────────────────

func TestInferMIMEFromURL_Extensions(t *testing.T) {
	cases := []struct{ url, want string }{
		{"file.png", "image/png"},
		{"file.jpg", "image/jpeg"},
		{"file.jpeg", "image/jpeg"},
		{"file.gif", "image/gif"},
		{"file.webp", "image/webp"},
		{"file.wav", "audio/wav"},
		{"file.mp3", "audio/mpeg"},
		{"file.m4a", "audio/mp4"},
		{"file.ogg", "audio/ogg"},
		{"file.mp4", "video/mp4"},
		{"file.mov", "video/quicktime"},
		{"file.webm", "video/webm"},
	}
	for _, tc := range cases {
		got := inferMIMEFromURL(tc.url, model.ContentPartInputImage)
		if got != tc.want {
			t.Errorf("url=%q: want %q got %q", tc.url, tc.want, got)
		}
	}
}

func TestInferMIMEFromURL_FallbackByType(t *testing.T) {
	cases := []struct {
		typ  model.ContentPartType
		want string
	}{
		{model.ContentPartInputImage, "image/jpeg"},
		{model.ContentPartOutputImage, "image/jpeg"},
		{model.ContentPartInputAudio, "audio/wav"},
		{model.ContentPartOutputAudio, "audio/wav"},
		{model.ContentPartInputVideo, "video/mp4"},
		{model.ContentPartOutputVideo, "video/mp4"},
		{model.ContentPartText, "application/octet-stream"},
	}
	for _, tc := range cases {
		got := inferMIMEFromURL("noext", tc.typ)
		if got != tc.want {
			t.Errorf("type=%q: want %q got %q", tc.typ, tc.want, got)
		}
	}
}

// ── inlinePartToContentPart ───────────────────────────────────────────────────

func TestInlinePartToContentPart(t *testing.T) {
	data := []byte("raw")
	b64 := base64.StdEncoding.EncodeToString(data)

	cases := []struct {
		mime    string
		wantTyp model.ContentPartType
	}{
		{"image/png", model.ContentPartOutputImage},
		{"audio/wav", model.ContentPartOutputAudio},
		{"video/mp4", model.ContentPartOutputVideo},
		{"application/octet-stream", model.ContentPartOutputImage}, // default
	}
	for _, tc := range cases {
		cp := inlinePartToContentPart(&genai.Blob{MIMEType: tc.mime, Data: data})
		if cp.Type != tc.wantTyp {
			t.Errorf("mime=%q: want type %q got %q", tc.mime, tc.wantTyp, cp.Type)
		}
		if cp.DataBase64 != b64 {
			t.Errorf("mime=%q: unexpected DataBase64", tc.mime)
		}
	}
}

// ── fromGeminiResponse edge cases ────────────────────────────────────────────

func TestFromGeminiResponse_Nil(t *testing.T) {
	resp := fromGeminiResponse(nil)
	if resp.Message.Role != model.RoleAssistant {
		t.Fatalf("unexpected role: %q", resp.Message.Role)
	}
	if len(resp.Message.ContentParts) != 0 {
		t.Fatalf("expected no content parts")
	}
}

func TestFromGeminiResponse_EmptyCandidates(t *testing.T) {
	resp := fromGeminiResponse(&genai.GenerateContentResponse{})
	if resp.Message.Role != model.RoleAssistant {
		t.Fatalf("unexpected role: %q", resp.Message.Role)
	}
}

// ── fromGeminiCandidate ───────────────────────────────────────────────────────

func TestFromGeminiCandidate_Nil(t *testing.T) {
	parts, calls := fromGeminiCandidate(nil)
	if parts != nil || calls != nil {
		t.Fatal("expected nil for nil candidate")
	}
}

func TestFromGeminiCandidate_NilContent(t *testing.T) {
	parts, calls := fromGeminiCandidate(&genai.Candidate{Content: nil})
	if parts != nil || calls != nil {
		t.Fatal("expected nil for nil content")
	}
}

func TestFromGeminiCandidate_InlineData(t *testing.T) {
	raw := []byte("imgdata")
	c := &genai.Candidate{
		Content: &genai.Content{
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "image/png", Data: raw}},
			},
		},
	}
	parts, _ := fromGeminiCandidate(c)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != model.ContentPartOutputImage {
		t.Fatalf("unexpected type: %q", parts[0].Type)
	}
}

func TestFromGeminiCandidate_FunctionCallNoID(t *testing.T) {
	c := &genai.Candidate{
		Content: &genai.Content{
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "", // no ID → fallback to "call_0"
						Name: "tool_x",
						Args: map[string]any{"a": 1},
					},
				},
			},
		},
	}
	_, calls := fromGeminiCandidate(c)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_0" {
		t.Fatalf("unexpected fallback id: %q", calls[0].ID)
	}
}

// ── collectToolCallNames ──────────────────────────────────────────────────────

func TestCollectToolCallNames(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("q")}},
		{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{
				{ID: "c1", Name: "fn1"},
				{ID: "", Name: "fn2"},    // empty id → skip
				{ID: "c3", Name: ""},     // empty name → skip
			},
		},
	}
	names := collectToolCallNames(msgs)
	if names["c1"] != "fn1" {
		t.Fatalf("expected fn1, got %q", names["c1"])
	}
	if _, ok := names[""]; ok {
		t.Fatal("empty id should not be stored")
	}
}

// ── toGeminiContents extra paths ──────────────────────────────────────────────

func TestToGeminiContents_Empty(t *testing.T) {
	system, contents, err := toGeminiContents(nil, DefaultModel)
	if err != nil || system != nil || len(contents) != 0 {
		t.Fatalf("unexpected: system=%v contents=%v err=%v", system, contents, err)
	}
}

func TestToGeminiContents_AssistantTextOnly(t *testing.T) {
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("hello")}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Role != genai.RoleModel {
		t.Fatalf("unexpected: %+v", contents)
	}
}

func TestToGeminiContents_AssistantReasoningSkipped(t *testing.T) {
	_, contents, err := toGeminiContents([]model.Message{
		{
			Role: model.RoleAssistant,
			ContentParts: []model.ContentPart{
				{Type: model.ContentPartReasoning, Text: "thinking..."},
				model.TextPart("answer"),
			},
		},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if len(contents[0].Parts) != 1 || contents[0].Parts[0].Text != "answer" {
		t.Fatalf("reasoning should be skipped, got: %+v", contents[0].Parts)
	}
}

func TestToGeminiContents_AssistantUnsupportedPart(t *testing.T) {
	_, _, err := toGeminiContents([]model.Message{
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{
			{Type: model.ContentPartInputImage},
		}},
	}, DefaultModel)
	if err == nil {
		t.Fatal("expected error for unsupported assistant part")
	}
	if !strings.Contains(err.Error(), "unsupported content part") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToGeminiContents_UnsupportedUserPart(t *testing.T) {
	_, _, err := toGeminiContents([]model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{
			{Type: model.ContentPartOutputImage},
		}},
	}, DefaultModel)
	if err == nil {
		t.Fatal("expected error for unsupported user part")
	}
}

func TestToGeminiContents_ToolResultFallbackName(t *testing.T) {
	// Tool result call ID not in assistant messages → fallback to call ID as name
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{
			{CallID: "orphan_call", ContentParts: []model.ContentPart{model.TextPart("result")}},
		}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Parts[0].FunctionResponse.Name != "orphan_call" {
		t.Fatalf("unexpected: %+v", contents)
	}
}

func TestToGeminiContents_ToolResultError(t *testing.T) {
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{
			{CallID: "c1", IsError: true, ContentParts: []model.ContentPart{model.TextPart("fail")}},
		}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	resp := contents[0].Parts[0].FunctionResponse.Response
	if resp["error"] != "fail" {
		t.Fatalf("expected error in response, got: %v", resp)
	}
}

func TestToGeminiContents_UserAudio(t *testing.T) {
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{
			model.MediaInlinePart(model.ContentPartInputAudio, "audio/wav", base64.StdEncoding.EncodeToString([]byte("wav")), ""),
		}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Parts[0].InlineData == nil {
		t.Fatalf("expected inline audio part: %+v", contents)
	}
}

func TestToGeminiContents_UserMediaURL(t *testing.T) {
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{
			{Type: model.ContentPartInputImage, URL: "https://example.com/img.png"},
		}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Parts[0].FileData == nil {
		t.Fatalf("expected file data part: %+v", contents)
	}
}

func TestToGeminiContents_ToolResultWithOutputImage(t *testing.T) {
	imgData := base64.StdEncoding.EncodeToString([]byte("img"))
	_, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{
			{CallID: "c1", ContentParts: []model.ContentPart{
				model.MediaInlinePart(model.ContentPartOutputImage, "image/png", imgData, ""),
			}},
		}},
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	parts := contents[0].Parts[0].FunctionResponse.Parts
	if len(parts) != 1 {
		t.Fatalf("expected 1 function response part, got %d", len(parts))
	}
}

func TestToGeminiContents_ToolResultUnsupportedPart(t *testing.T) {
	_, _, err := toGeminiContents([]model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{
			{CallID: "c1", ContentParts: []model.ContentPart{
				{Type: model.ContentPartInputImage},
			}},
		}},
	}, DefaultModel)
	if err == nil {
		t.Fatal("expected error for unsupported tool result part")
	}
}

// ── toGeminiTool error path ───────────────────────────────────────────────────

func TestToGeminiTool_InvalidSchema(t *testing.T) {
	_, err := toGeminiTool([]model.ToolSpec{
		{Name: "bad", InputSchema: json.RawMessage(`not-json`)},
	})
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

// ── toGeminiMediaPart ─────────────────────────────────────────────────────────

func TestToGeminiMediaPart_URL(t *testing.T) {
	p, err := toGeminiMediaPart(model.ContentPart{
		Type: model.ContentPartInputImage,
		URL:  "https://example.com/img.jpg",
	}, "user", DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if p.FileData == nil {
		t.Fatal("expected FileData for URL part")
	}
}

func TestToGeminiMediaPart_URL_NoMIME(t *testing.T) {
	// URL with no extension + no MIMEType → inferred from type
	p, err := toGeminiMediaPart(model.ContentPart{
		Type: model.ContentPartInputImage,
		URL:  "https://example.com/image",
	}, "user", DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if p.FileData == nil {
		t.Fatal("expected FileData for URL part")
	}
}

func TestToGeminiMediaPart_InvalidBase64(t *testing.T) {
	_, err := toGeminiMediaPart(model.ContentPart{
		Type:       model.ContentPartInputImage,
		DataBase64: "!!!not-base64!!!",
	}, "user", DefaultModel)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// ── toGeminiFunctionResponsePart ──────────────────────────────────────────────

func TestToGeminiFunctionResponsePart_URL(t *testing.T) {
	p, err := toGeminiFunctionResponsePart(model.ContentPart{
		Type: model.ContentPartOutputImage,
		URL:  "https://example.com/out.png",
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil part")
	}
}

func TestToGeminiFunctionResponsePart_Inline(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("data"))
	p, err := toGeminiFunctionResponsePart(model.ContentPart{
		Type:       model.ContentPartOutputAudio,
		MIMEType:   "audio/wav",
		DataBase64: raw,
	}, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil part")
	}
}

func TestToGeminiFunctionResponsePart_InvalidBase64(t *testing.T) {
	_, err := toGeminiFunctionResponsePart(model.ContentPart{
		Type:       model.ContentPartOutputImage,
		DataBase64: "!!!",
	}, DefaultModel)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// ── streamIterator dedup / close ──────────────────────────────────────────────

func TestStreamIterator_DedupToolCall(t *testing.T) {
	it := &streamIterator{seenToolCalls: make(map[string]struct{})}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "fn", Args: map[string]any{"x": 1}}},
			}}},
		},
	}
	it.processResponse(resp)
	it.processResponse(resp) // same call again → should dedup
	toolCalls := 0
	for _, c := range it.pending {
		if c.ToolCall != nil {
			toolCalls++
		}
	}
	if toolCalls != 1 {
		t.Fatalf("expected 1 unique tool call after dedup, got %d", toolCalls)
	}
}

func TestStreamIterator_Close(t *testing.T) {
	stopped := false
	it := &streamIterator{
		seenToolCalls: make(map[string]struct{}),
		stop:          func() { stopped = true },
	}
	it.Close()
	if !stopped {
		t.Fatal("expected stop to be called")
	}
	if !it.done {
		t.Fatal("expected done=true after Close")
	}
	// Second close should not panic or double-call stop
	it.Close()
}

func TestStreamIterator_ProcessResponse_NilPart(t *testing.T) {
	it := &streamIterator{seenToolCalls: make(map[string]struct{})}
	it.processResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{nil}}},
		},
	})
	if len(it.pending) != 0 {
		t.Fatalf("nil part should produce no chunks, got %d", len(it.pending))
	}
}

func TestStreamIterator_ProcessResponse_NilResp(t *testing.T) {
	it := &streamIterator{seenToolCalls: make(map[string]struct{})}
	it.processResponse(nil) // should not panic
}

// ── contentPartsToTextOnlyString ──────────────────────────────────────────────

func TestContentPartsToTextOnlyString(t *testing.T) {
	parts := []model.ContentPart{
		model.TextPart("hello"),
		model.TextPart("world"),
	}
	got, err := contentPartsToTextOnlyString(parts, "gemini", DefaultModel, "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\nworld" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestContentPartsToTextOnlyString_ReasoningSkippedForAssistant(t *testing.T) {
	parts := []model.ContentPart{
		{Type: model.ContentPartReasoning, Text: "thinking"},
		model.TextPart("answer"),
	}
	got, err := contentPartsToTextOnlyString(parts, "gemini", DefaultModel, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if got != "answer" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestContentPartsToTextOnlyString_UnsupportedPart(t *testing.T) {
	parts := []model.ContentPart{{Type: model.ContentPartInputImage}}
	_, err := contentPartsToTextOnlyString(parts, "gemini", DefaultModel, "user")
	if err == nil {
		t.Fatal("expected error for non-text part")
	}
}

// ── existing tests ────────────────────────────────────────────────────────────

func TestToGeminiContents_UserMultimodal(t *testing.T) {
	system, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys")}},
		{
			Role: model.RoleUser,
			ContentParts: []model.ContentPart{
				model.TextPart("describe"),
				model.MediaInlinePart(model.ContentPartInputImage, "image/png", base64.StdEncoding.EncodeToString([]byte("img")), "a.png"),
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
	system, contents, err := toGeminiContents([]model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("weather?")}},
		{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{
				{ID: "c1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"beijing"}`)},
			},
		},
		{
			Role: model.RoleTool,
			ToolResults: []model.ToolResult{
				{CallID: "c1", ContentParts: []model.ContentPart{model.TextPart("sunny")}},
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
	tool, err := toGeminiTool([]model.ToolSpec{
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
	if got := model.ContentPartsToPlainText(resp.Message.ContentParts); got != "hello" {
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
