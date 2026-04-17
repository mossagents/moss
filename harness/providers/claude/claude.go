package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/mossagents/moss/kernel/model"
	"io"
	"iter"
	"net/http"
	"strings"
)

// 确保实现 model.LLM 接口。
var _ model.LLM = (*Client)(nil)

const DefaultModel = "claude-sonnet-4-20250514"

// Client 是 Claude LLM 适配器。
type Client struct {
	client    anthropic.Client
	model     string
	maxTokens int64
}

// Option 是 Client 的配置选项。
type Option func(*Client)

// WithModel 设置默认模型。
func WithModel(model string) Option { return func(c *Client) { c.model = model } }

// WithMaxTokens 设置默认最大 token 数。
func WithMaxTokens(n int64) Option { return func(c *Client) { c.maxTokens = n } }

// New 创建 Claude 适配器。apiKey 为空时从 ANTHROPIC_API_KEY 环境变量读取。
func New(apiKey string, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	c := &Client{
		client:    anthropic.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 8192,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewWithHTTPClient creates a Claude adapter using a custom http.Client.
// Use this to inject a tracing transport for distributed trace propagation:
//
//	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
//	adapter := claude.NewWithHTTPClient(apiKey, &http.Client{Transport: transport})
func NewWithHTTPClient(apiKey string, httpClient *http.Client, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		reqOpts = append(reqOpts, option.WithHTTPClient(httpClient))
	}
	c := &Client{
		client:    anthropic.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 8192,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewWithBaseURL 创建 Claude 适配器，允许指定 API Key 和 Base URL。
func NewWithBaseURL(apiKey, baseURL string, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(baseURL))
	}
	c := &Client{
		client:    anthropic.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 8192,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GenerateContent 实现 model.LLM（统一流式接口）。
func (c *Client) GenerateContent(ctx context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		params, err := c.buildParams(req)
		if err != nil {
			yield(model.StreamChunk{}, err)
			return
		}
		stream := c.client.Messages.NewStreaming(ctx, params)
		si := &streamIterator{
			stream:          stream,
			toolUseBuilders: make(map[int]*toolUseBuilder),
		}
		defer si.Close()

		for {
			chunk, err := si.Next()
			if err == io.EOF {
				return
			}
			if !yield(chunk, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// buildParams 构建 Anthropic API 请求参数。
func (c *Client) buildParams(req model.CompletionRequest) (anthropic.MessageNewParams, error) {
	model := c.model
	maxTokens := c.maxTokens
	if req.Config.Model != "" {
		model = req.Config.Model
	}
	if req.Config.MaxTokens > 0 {
		maxTokens = int64(req.Config.MaxTokens)
	}

	system, messages, err := toAnthropicMessages(req.Messages, model)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	tools, err := toAnthropicTools(req.Tools)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	params := anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if len(system) > 0 {
		params.System = system
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	if req.Config.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Config.Temperature)
	}
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			params.OutputConfig = anthropic.OutputConfigParam{
				Format: anthropic.JSONOutputFormatParam{
					Schema: map[string]any{"type": "object"},
				},
			}
		case "json_schema":
			var schema map[string]any
			if req.ResponseFormat.JSONSchema != nil && len(req.ResponseFormat.JSONSchema.Schema) > 0 {
				if err := json.Unmarshal(req.ResponseFormat.JSONSchema.Schema, &schema); err != nil {
					return anthropic.MessageNewParams{}, fmt.Errorf("unmarshal json_schema: %w", err)
				}
			}
			if len(schema) == 0 {
				schema = map[string]any{"type": "object"}
			}
			params.OutputConfig = anthropic.OutputConfigParam{
				Format: anthropic.JSONOutputFormatParam{Schema: schema},
			}
		}
	}
	return params, nil
}

// ─── 消息映射 ────────────────────────────────────────

// normalizeMessages merges consecutive messages with the same role, excluding
// system and tool messages, to prevent API-level validation errors from strict
// providers such as DeepSeek that forbid consecutive same-role turns.
func normalizeMessages(msgs []model.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]model.Message, 0, len(msgs))
	for _, msg := range msgs {
		if len(out) > 0 &&
			msg.Role != model.RoleSystem &&
			msg.Role != model.RoleTool &&
			out[len(out)-1].Role == msg.Role {
			last := &out[len(out)-1]
			last.ContentParts = append(last.ContentParts, msg.ContentParts...)
			last.ToolCalls = append(last.ToolCalls, msg.ToolCalls...)
		} else {
			out = append(out, msg)
		}
	}
	return out
}

func toAnthropicMessages(msgs []model.Message, modelName string) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	msgs = normalizeMessages(msgs)
	var system []anthropic.TextBlockParam
	var messages []anthropic.MessageParam

	for _, msg := range msgs {
		switch msg.Role {
		case model.RoleSystem:
			text, err := contentPartsToTextOnlyString(msg.ContentParts, "claude", modelName, "system")
			if err != nil {
				return nil, nil, err
			}
			system = append(system, anthropic.TextBlockParam{Text: text})
		case model.RoleUser:
			blocks, err := toAnthropicUserBlocks(msg.ContentParts, modelName)
			if err != nil {
				return nil, nil, err
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		case model.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			content, err := contentPartsToTextOnlyString(msg.ContentParts, "claude", modelName, "assistant")
			if err != nil {
				return nil, nil, err
			}
			if content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(content))
			}
			for _, tc := range msg.ToolCalls {
				var input any
				if err := json.Unmarshal(tc.Arguments, &input); err != nil {
					return nil, nil, fmt.Errorf("unmarshal tool call arguments for %s: %w", tc.Name, err)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		case model.RoleTool:
			var blocks []anthropic.ContentBlockParamUnion
			for _, tr := range msg.ToolResults {
				contentBlocks, err := toAnthropicToolResultBlocks(tr.ContentParts, modelName)
				if err != nil {
					return nil, nil, err
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.CallID,
						IsError:   anthropic.Bool(tr.IsError),
						Content:   contentBlocks,
					},
				})
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewUserMessage(blocks...))
			}
		}
	}
	return system, messages, nil
}

func toAnthropicTools(tools []model.ToolSpec) ([]anthropic.ToolUnionParam, error) {
	result := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		var schema anthropic.ToolInputSchemaParam
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("unmarshal tool schema for %s: %w", t.Name, err)
		}

		tp := anthropic.ToolParam{
			Name:        t.Name,
			InputSchema: schema,
		}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		result[i] = anthropic.ToolUnionParam{OfTool: &tp}
	}
	return result, nil
}

// ─── 响应映射 ────────────────────────────────────────

func fromAnthropicResponse(msg *anthropic.Message) *model.CompletionResponse {
	var contentParts []model.ContentPart
	var toolCalls []model.ToolCall

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if strings.TrimSpace(v.Text) != "" {
				contentParts = append(contentParts, model.TextPart(v.Text))
			}
		case anthropic.ToolUseBlock:
			toolCalls = append(toolCalls, model.ToolCall{
				ID:        v.ID,
				Name:      v.Name,
				Arguments: v.Input,
			})
		}
	}

	return &model.CompletionResponse{
		Message: model.Message{
			Role:         model.RoleAssistant,
			ContentParts: contentParts,
			ToolCalls:    toolCalls,
		},
		ToolCalls: toolCalls,
		Usage: model.TokenUsage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
		StopReason: string(msg.StopReason),
	}
}

func toAnthropicUserBlocks(parts []model.ContentPart, modelName string) ([]anthropic.ContentBlockParamUnion, error) {
	result := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case model.ContentPartText:
			result = append(result, anthropic.NewTextBlock(part.Text))
		case model.ContentPartInputImage:
			block, err := toAnthropicImageBlock(part)
			if err != nil {
				return nil, err
			}
			result = append(result, block)
		case model.ContentPartInputAudio:
			return nil, capabilityUnavailableError("claude", modelName, "user", part.Type, "audio input is not supported by current anthropic content blocks")
		case model.ContentPartInputVideo:
			block, err := toAnthropicVideoBlock(part)
			if err != nil {
				return nil, err
			}
			result = append(result, block)
		default:
			return nil, unsupportedPartError("claude", modelName, "user", part.Type)
		}
	}
	return result, nil
}

func toAnthropicToolResultBlocks(parts []model.ContentPart, modelName string) ([]anthropic.ToolResultBlockParamContentUnion, error) {
	result := make([]anthropic.ToolResultBlockParamContentUnion, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case model.ContentPartText:
			result = append(result, anthropic.ToolResultBlockParamContentUnion{
				OfText: &anthropic.TextBlockParam{Text: part.Text},
			})
		case model.ContentPartOutputImage:
			block, err := toAnthropicImageBlock(part)
			if err != nil {
				return nil, err
			}
			result = append(result, anthropic.ToolResultBlockParamContentUnion{
				OfImage: block.OfImage,
			})
		case model.ContentPartOutputAudio:
			return nil, capabilityUnavailableError("claude", modelName, "tool_result", part.Type, "audio output blocks are not supported by current anthropic content blocks")
		case model.ContentPartOutputVideo:
			return nil, capabilityUnavailableError("claude", modelName, "tool_result", part.Type, "video output blocks are not supported by current anthropic content blocks")
		default:
			return nil, unsupportedPartError("claude", modelName, "tool_result", part.Type)
		}
	}
	return result, nil
}

func toAnthropicImageBlock(part model.ContentPart) (anthropic.ContentBlockParamUnion, error) {
	if strings.TrimSpace(part.URL) != "" {
		return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: part.URL}), nil
	}
	mt := anthropic.Base64ImageSourceMediaType(part.MIMEType)
	switch mt {
	case anthropic.Base64ImageSourceMediaTypeImageJPEG,
		anthropic.Base64ImageSourceMediaTypeImagePNG,
		anthropic.Base64ImageSourceMediaTypeImageGIF,
		anthropic.Base64ImageSourceMediaTypeImageWebP:
		return anthropic.NewImageBlock(anthropic.Base64ImageSourceParam{
			MediaType: mt,
			Data:      part.DataBase64,
		}), nil
	default:
		return anthropic.ContentBlockParamUnion{}, fmt.Errorf("claude adapter: unsupported image mime_type=%q", part.MIMEType)
	}
}

func toAnthropicVideoBlock(part model.ContentPart) (anthropic.ContentBlockParamUnion, error) {
	if strings.TrimSpace(part.URL) != "" {
		return anthropic.NewDocumentBlock(anthropic.URLPDFSourceParam{URL: part.URL}), nil
	}
	return anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
		Data: part.DataBase64,
	}), nil
}

func contentPartsToTextOnlyString(parts []model.ContentPart, provider, modelName, role string) (string, error) {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if role == "assistant" && part.Type == model.ContentPartReasoning {
			continue
		}
		if part.Type != model.ContentPartText && part.Type != model.ContentPartRefusal {
			return "", unsupportedPartError(provider, modelName, role, part.Type)
		}
		textParts = append(textParts, part.Text)
	}
	return strings.Join(textParts, "\n"), nil
}

func unsupportedPartError(provider, model, role string, typ model.ContentPartType) error {
	return fmt.Errorf("%s adapter: model=%q role=%s unsupported content part type=%q", provider, model, role, typ)
}

func capabilityUnavailableError(provider, model, role string, typ model.ContentPartType, reason string) error {
	return fmt.Errorf("%s adapter: model=%q role=%s content part type=%q capability unavailable: %s", provider, model, role, typ, reason)
}

// ─── 流式迭代器 ──────────────────────────────────────

type toolUseBuilder struct {
	id    string
	name  string
	input string
}

type streamIterator struct {
	stream          *ssestream.Stream[anthropic.MessageStreamEventUnion]
	pending         []model.StreamChunk
	done            bool
	usage           model.TokenUsage
	emittedToolUse  bool
	toolUseBuilders map[int]*toolUseBuilder
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
		if !it.stream.Next() {
			if err := it.stream.Err(); err != nil {
				return model.StreamChunk{}, err
			}
			it.done = true
			return model.StreamChunk{}, io.EOF
		}
		it.processEvent(it.stream.Current())
	}
}

func (it *streamIterator) processEvent(event anthropic.MessageStreamEventUnion) {
	switch e := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		it.usage.PromptTokens = int(e.Message.Usage.InputTokens)

	case anthropic.ContentBlockStartEvent:
		if e.ContentBlock.Type == "tool_use" {
			it.toolUseBuilders[int(e.Index)] = &toolUseBuilder{
				id:   e.ContentBlock.ID,
				name: e.ContentBlock.Name,
			}
		}

	case anthropic.ContentBlockDeltaEvent:
		switch d := e.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			it.pending = append(it.pending, model.TextDeltaChunk(d.Text))
		case anthropic.InputJSONDelta:
			if tb, ok := it.toolUseBuilders[int(e.Index)]; ok {
				tb.input += d.PartialJSON
			}
		}

	case anthropic.ContentBlockStopEvent:
		if tb, ok := it.toolUseBuilders[int(e.Index)]; ok {
			input := tb.input
			if input == "" {
				input = "{}"
			}
			tc := model.ToolCall{
				ID:        tb.id,
				Name:      tb.name,
				Arguments: json.RawMessage(input),
			}
			it.pending = append(it.pending, model.ToolCallChunk(&tc))
			it.emittedToolUse = true
			delete(it.toolUseBuilders, int(e.Index))
		}

	case anthropic.MessageDeltaEvent:
		it.usage.CompletionTokens = int(e.Usage.OutputTokens)
		it.usage.TotalTokens = it.usage.PromptTokens + it.usage.CompletionTokens

	case anthropic.MessageStopEvent:
		stopReason := "end_turn"
		if it.emittedToolUse {
			stopReason = "tool_use"
		}
		it.pending = append(it.pending, model.DoneChunk(stopReason, &it.usage, nil))
	}
}

func (it *streamIterator) Close() error {
	it.done = true
	return nil
}
