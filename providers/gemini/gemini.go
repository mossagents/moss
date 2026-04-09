package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/model"
	"google.golang.org/genai"
	"io"
	"iter"
	"net/http"
	"path"
	"strings"
)

var (
	_ model.LLM          = (*Client)(nil)
	_ model.StreamingLLM = (*Client)(nil)
)

const DefaultModel = "gemini-2.5-flash"

type Client struct {
	client    *genai.Client
	model     string
	maxTokens int32
	initErr   error
}

type Option func(*Client)

func WithModel(model string) Option { return func(c *Client) { c.model = model } }

func WithMaxTokens(n int32) Option { return func(c *Client) { c.maxTokens = n } }

func New(apiKey string, opts ...Option) *Client {
	return newClient(apiKey, "", nil, opts...)
}

func NewWithHTTPClient(apiKey string, httpClient *http.Client, opts ...Option) *Client {
	return newClient(apiKey, "", httpClient, opts...)
}

func NewWithBaseURL(apiKey, baseURL string, opts ...Option) *Client {
	return newClient(apiKey, baseURL, nil, opts...)
}

func newClient(apiKey, baseURL string, httpClient *http.Client, opts ...Option) *Client {
	c := &Client{
		model:     DefaultModel,
		maxTokens: 8192,
	}
	for _, opt := range opts {
		opt(c)
	}
	cc := &genai.ClientConfig{
		Backend: genai.BackendGeminiAPI,
	}
	if strings.TrimSpace(apiKey) != "" {
		cc.APIKey = strings.TrimSpace(apiKey)
	}
	if httpClient != nil {
		cc.HTTPClient = httpClient
	}
	if strings.TrimSpace(baseURL) != "" {
		cc.HTTPOptions.BaseURL = strings.TrimSpace(baseURL)
	}
	client, err := genai.NewClient(context.Background(), cc)
	if err != nil {
		c.initErr = err
		return c
	}
	c.client = client
	return c
}

func (c *Client) Complete(ctx context.Context, req model.CompletionRequest) (*model.CompletionResponse, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	system, contents, err := toGeminiContents(req.Messages, c.model)
	if err != nil {
		return nil, err
	}
	cfg, err := c.buildConfig(req, system)
	if err != nil {
		return nil, err
	}
	model := c.effectiveModel(req)
	resp, err := c.client.Models.GenerateContent(ctx, model, contents, cfg)
	if err != nil {
		return nil, err
	}
	return fromGeminiResponse(resp), nil
}

func (c *Client) Stream(ctx context.Context, req model.CompletionRequest) (model.StreamIterator, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	system, contents, err := toGeminiContents(req.Messages, c.model)
	if err != nil {
		return nil, err
	}
	cfg, err := c.buildConfig(req, system)
	if err != nil {
		return nil, err
	}
	model := c.effectiveModel(req)
	next, stop := iter.Pull2(c.client.Models.GenerateContentStream(ctx, model, contents, cfg))
	return &streamIterator{
		next:          next,
		stop:          stop,
		seenToolCalls: make(map[string]struct{}),
	}, nil
}

func (c *Client) effectiveModel(req model.CompletionRequest) string {
	if req.Config.Model != "" {
		return req.Config.Model
	}
	return c.model
}

func (c *Client) buildConfig(req model.CompletionRequest, system *genai.Content) (*genai.GenerateContentConfig, error) {
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: system,
	}
	maxTokens := c.maxTokens
	if req.Config.MaxTokens > 0 {
		maxTokens = int32(req.Config.MaxTokens)
	}
	if maxTokens > 0 {
		cfg.MaxOutputTokens = maxTokens
	}
	if req.Config.Temperature > 0 {
		temp := float32(req.Config.Temperature)
		cfg.Temperature = &temp
	}
	if len(req.Tools) > 0 {
		tool, err := toGeminiTool(req.Tools)
		if err != nil {
			return nil, err
		}
		cfg.Tools = []*genai.Tool{tool}
	}
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			cfg.ResponseMIMEType = "application/json"
		case "json_schema":
			cfg.ResponseMIMEType = "application/json"
			if req.ResponseFormat.JSONSchema != nil && len(req.ResponseFormat.JSONSchema.Schema) > 0 {
				var schema any
				if err := json.Unmarshal(req.ResponseFormat.JSONSchema.Schema, &schema); err != nil {
					return nil, fmt.Errorf("gemini adapter: invalid response json schema: %w", err)
				}
				cfg.ResponseJsonSchema = schema
			}
		}
	}
	return cfg, nil
}

func toGeminiTool(tools []model.ToolSpec) (*genai.Tool, error) {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		var params any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("gemini adapter: invalid tool schema for %q: %w", t.Name, err)
			}
		}
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name,
			Description:          t.Description,
			ParametersJsonSchema: params,
		})
	}
	return &genai.Tool{FunctionDeclarations: decls}, nil
}

func toGeminiContents(msgs []model.Message, defaultModel string) (*genai.Content, []*genai.Content, error) {
	var systemTexts []string
	var contents []*genai.Content
	nameByCallID := collectToolCallNames(msgs)

	for _, msg := range msgs {
		switch msg.Role {
		case model.RoleSystem:
			text, err := contentPartsToTextOnlyString(msg.ContentParts, "gemini", defaultModel, "system")
			if err != nil {
				return nil, nil, err
			}
			if strings.TrimSpace(text) != "" {
				systemTexts = append(systemTexts, text)
			}

		case model.RoleUser:
			parts, err := toGeminiUserParts(msg.ContentParts, defaultModel)
			if err != nil {
				return nil, nil, err
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleUser,
					Parts: parts,
				})
			}

		case model.RoleAssistant:
			parts, err := toGeminiAssistantParts(msg, defaultModel)
			if err != nil {
				return nil, nil, err
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleModel,
					Parts: parts,
				})
			}

		case model.RoleTool:
			parts, err := toGeminiToolResultParts(msg.ToolResults, nameByCallID, defaultModel)
			if err != nil {
				return nil, nil, err
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleUser,
					Parts: parts,
				})
			}
		}
	}

	var system *genai.Content
	if len(systemTexts) > 0 {
		system = &genai.Content{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				genai.NewPartFromText(strings.Join(systemTexts, "\n")),
			},
		}
	}
	return system, contents, nil
}

func collectToolCallNames(msgs []model.Message) map[string]string {
	result := make(map[string]string)
	for _, msg := range msgs {
		if msg.Role != model.RoleAssistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if strings.TrimSpace(tc.ID) == "" || strings.TrimSpace(tc.Name) == "" {
				continue
			}
			result[tc.ID] = tc.Name
		}
	}
	return result
}

func toGeminiUserParts(parts []model.ContentPart, modelName string) ([]*genai.Part, error) {
	result := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case model.ContentPartText:
			result = append(result, genai.NewPartFromText(part.Text))
		case model.ContentPartInputImage, model.ContentPartInputAudio, model.ContentPartInputVideo:
			p, err := toGeminiMediaPart(part, "user", modelName)
			if err != nil {
				return nil, err
			}
			result = append(result, p)
		default:
			return nil, unsupportedPartError("gemini", modelName, "user", part.Type)
		}
	}
	return result, nil
}

func toGeminiAssistantParts(msg model.Message, modelName string) ([]*genai.Part, error) {
	parts := make([]*genai.Part, 0, len(msg.ContentParts)+len(msg.ToolCalls))
	for _, cp := range msg.ContentParts {
		if cp.Type == model.ContentPartReasoning {
			continue
		}
		if cp.Type != model.ContentPartText {
			return nil, unsupportedPartError("gemini", modelName, "assistant", cp.Type)
		}
		parts = append(parts, genai.NewPartFromText(cp.Text))
	}
	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if len(tc.Arguments) > 0 {
			if err := json.Unmarshal(tc.Arguments, &args); err != nil {
				return nil, fmt.Errorf("gemini adapter: invalid assistant tool_call arguments for %q: %w", tc.Name, err)
			}
		}
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: args,
			},
		})
	}
	return parts, nil
}

func toGeminiToolResultParts(results []model.ToolResult, nameByCallID map[string]string, modelName string) ([]*genai.Part, error) {
	parts := make([]*genai.Part, 0, len(results))
	for _, tr := range results {
		name := strings.TrimSpace(nameByCallID[tr.CallID])
		if name == "" {
			name = fallbackToolName(tr.CallID)
		}

		outputText := make([]string, 0, len(tr.ContentParts))
		functionParts := make([]*genai.FunctionResponsePart, 0)
		for _, cp := range tr.ContentParts {
			switch cp.Type {
			case model.ContentPartText:
				if strings.TrimSpace(cp.Text) != "" {
					outputText = append(outputText, cp.Text)
				}
			case model.ContentPartOutputImage, model.ContentPartOutputAudio, model.ContentPartOutputVideo:
				fp, err := toGeminiFunctionResponsePart(cp, modelName)
				if err != nil {
					return nil, err
				}
				functionParts = append(functionParts, fp)
			default:
				return nil, unsupportedPartError("gemini", modelName, "tool_result", cp.Type)
			}
		}

		responsePayload := map[string]any{
			"output": strings.Join(outputText, "\n"),
		}
		if tr.IsError {
			responsePayload["error"] = strings.Join(outputText, "\n")
		}
		parts = append(parts, &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       tr.CallID,
				Name:     name,
				Response: responsePayload,
				Parts:    functionParts,
			},
		})
	}
	return parts, nil
}

func fallbackToolName(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return "tool_result"
	}
	return callID
}

func toGeminiMediaPart(part model.ContentPart, role, modelName string) (*genai.Part, error) {
	mimeType := strings.TrimSpace(part.MIMEType)
	if strings.TrimSpace(part.URL) != "" {
		if mimeType == "" {
			mimeType = inferMIMEFromURL(part.URL, part.Type)
		}
		return genai.NewPartFromURI(part.URL, mimeType), nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part.DataBase64))
	if err != nil {
		return nil, fmt.Errorf("gemini adapter: modelName=%q role=%s invalid base64 for %s: %w", modelName, role, part.Type, err)
	}
	return genai.NewPartFromBytes(raw, mimeType), nil
}

func toGeminiFunctionResponsePart(part model.ContentPart, modelName string) (*genai.FunctionResponsePart, error) {
	mimeType := strings.TrimSpace(part.MIMEType)
	if strings.TrimSpace(part.URL) != "" {
		if mimeType == "" {
			mimeType = inferMIMEFromURL(part.URL, part.Type)
		}
		return genai.NewFunctionResponsePartFromURI(part.URL, mimeType), nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part.DataBase64))
	if err != nil {
		return nil, fmt.Errorf("gemini adapter: modelName=%q role=tool_result invalid base64 for %s: %w", modelName, part.Type, err)
	}
	return genai.NewFunctionResponsePartFromBytes(raw, mimeType), nil
}

func inferMIMEFromURL(url string, typ model.ContentPartType) string {
	switch strings.ToLower(path.Ext(url)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	}
	switch typ {
	case model.ContentPartInputImage, model.ContentPartOutputImage:
		return "image/jpeg"
	case model.ContentPartInputAudio, model.ContentPartOutputAudio:
		return "audio/wav"
	case model.ContentPartInputVideo, model.ContentPartOutputVideo:
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

func fromGeminiResponse(resp *genai.GenerateContentResponse) *model.CompletionResponse {
	if resp == nil || len(resp.Candidates) == 0 {
		return &model.CompletionResponse{
			Message: model.Message{Role: model.RoleAssistant},
			Usage:   fromGeminiUsage(nil),
		}
	}
	candidate := resp.Candidates[0]
	contentParts, toolCalls := fromGeminiCandidate(candidate)
	return &model.CompletionResponse{
		Message: model.Message{
			Role:         model.RoleAssistant,
			ContentParts: contentParts,
			ToolCalls:    toolCalls,
		},
		ToolCalls:  toolCalls,
		Usage:      fromGeminiUsage(resp.UsageMetadata),
		StopReason: string(candidate.FinishReason),
	}
}

func fromGeminiCandidate(c *genai.Candidate) ([]model.ContentPart, []model.ToolCall) {
	var contentParts []model.ContentPart
	var toolCalls []model.ToolCall
	if c == nil || c.Content == nil {
		return nil, nil
	}
	for i, p := range c.Content.Parts {
		if p == nil {
			continue
		}
		if strings.TrimSpace(p.Text) != "" {
			contentParts = append(contentParts, model.TextPart(p.Text))
		}
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			if len(args) == 0 {
				args = []byte("{}")
			}
			id := strings.TrimSpace(p.FunctionCall.ID)
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			toolCalls = append(toolCalls, model.ToolCall{
				ID:        id,
				Name:      p.FunctionCall.Name,
				Arguments: json.RawMessage(args),
			})
		}
		if p.InlineData != nil {
			contentParts = append(contentParts, inlinePartToContentPart(p.InlineData))
		}
	}
	return contentParts, toolCalls
}

func inlinePartToContentPart(b *genai.Blob) model.ContentPart {
	mimeType := strings.TrimSpace(b.MIMEType)
	data := base64.StdEncoding.EncodeToString(b.Data)
	switch {
	case strings.HasPrefix(strings.ToLower(mimeType), "image/"):
		return model.MediaInlinePart(model.ContentPartOutputImage, mimeType, data, "")
	case strings.HasPrefix(strings.ToLower(mimeType), "audio/"):
		return model.MediaInlinePart(model.ContentPartOutputAudio, mimeType, data, "")
	case strings.HasPrefix(strings.ToLower(mimeType), "video/"):
		return model.MediaInlinePart(model.ContentPartOutputVideo, mimeType, data, "")
	default:
		return model.MediaInlinePart(model.ContentPartOutputImage, mimeType, data, "")
	}
}

func fromGeminiUsage(u *genai.GenerateContentResponseUsageMetadata) model.TokenUsage {
	if u == nil {
		return model.TokenUsage{}
	}
	return model.TokenUsage{
		PromptTokens:     int(u.PromptTokenCount),
		CompletionTokens: int(u.CandidatesTokenCount),
		TotalTokens:      int(u.TotalTokenCount),
	}
}

type streamIterator struct {
	next func() (*genai.GenerateContentResponse, error, bool)
	stop func()

	pending       []model.StreamChunk
	done          bool
	stopped       bool
	usage         model.TokenUsage
	seenToolCalls map[string]struct{}
}

func (it *streamIterator) Next() (model.StreamChunk, error) {
	for {
		if len(it.pending) > 0 {
			chunk := it.pending[0]
			it.pending = it.pending[1:]
			return chunk, nil
		}
		if it.done {
			return model.StreamChunk{}, io.EOF
		}

		resp, err, ok := it.next()
		if !ok {
			it.closeStop()
			it.done = true
			it.pending = append(it.pending, model.StreamChunk{
				Done:  true,
				Usage: &it.usage,
			})
			continue
		}
		if err != nil {
			it.closeStop()
			return model.StreamChunk{}, err
		}
		it.processResponse(resp)
	}
}

func (it *streamIterator) processResponse(resp *genai.GenerateContentResponse) {
	if resp == nil {
		return
	}
	it.usage = fromGeminiUsage(resp.UsageMetadata)
	if len(resp.Candidates) == 0 {
		return
	}
	c := resp.Candidates[0]
	if c == nil || c.Content == nil {
		return
	}
	for i, p := range c.Content.Parts {
		if p == nil {
			continue
		}
		if p.Text != "" {
			it.pending = append(it.pending, model.StreamChunk{Delta: p.Text})
		}
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			if len(args) == 0 {
				args = []byte("{}")
			}
			id := strings.TrimSpace(p.FunctionCall.ID)
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			key := id + "\x00" + p.FunctionCall.Name + "\x00" + string(args)
			if _, ok := it.seenToolCalls[key]; ok {
				continue
			}
			it.seenToolCalls[key] = struct{}{}
			tc := model.ToolCall{
				ID:        id,
				Name:      p.FunctionCall.Name,
				Arguments: json.RawMessage(args),
			}
			it.pending = append(it.pending, model.StreamChunk{ToolCall: &tc})
		}
	}
}

func (it *streamIterator) Close() error {
	it.done = true
	it.closeStop()
	return nil
}

func (it *streamIterator) closeStop() {
	if it.stopped {
		return
	}
	it.stopped = true
	if it.stop != nil {
		it.stop()
	}
}

func contentPartsToTextOnlyString(parts []model.ContentPart, provider, modelName, role string) (string, error) {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if role == "assistant" && part.Type == model.ContentPartReasoning {
			continue
		}
		if part.Type != model.ContentPartText {
			return "", unsupportedPartError(provider, modelName, role, part.Type)
		}
		textParts = append(textParts, part.Text)
	}
	return strings.Join(textParts, "\n"), nil
}

func unsupportedPartError(provider, model, role string, typ model.ContentPartType) error {
	return fmt.Errorf("%s adapter: model=%q role=%s unsupported content part type=%q", provider, model, role, typ)
}
