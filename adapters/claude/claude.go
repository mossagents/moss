package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/mossagents/moss/kernel/port"
)

// 确保实现 port.LLM 和 port.StreamingLLM 接口。
var (
	_ port.LLM          = (*Client)(nil)
	_ port.StreamingLLM = (*Client)(nil)
)

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

// Complete 实现 port.LLM（同步模式）。
func (c *Client) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return nil, err
	}
	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return fromAnthropicResponse(msg), nil
}

// Stream 实现 port.StreamingLLM（流式模式）。
func (c *Client) Stream(ctx context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return nil, err
	}
	stream := c.client.Messages.NewStreaming(ctx, params)
	return &streamIterator{
		stream:          stream,
		toolUseBuilders: make(map[int]*toolUseBuilder),
	}, nil
}

// buildParams 构建 Anthropic API 请求参数。
func (c *Client) buildParams(req port.CompletionRequest) (anthropic.MessageNewParams, error) {
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
	tools := toAnthropicTools(req.Tools)

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
				_ = json.Unmarshal(req.ResponseFormat.JSONSchema.Schema, &schema)
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

func toAnthropicMessages(msgs []port.Message, model string) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	var system []anthropic.TextBlockParam
	var messages []anthropic.MessageParam

	for _, msg := range msgs {
		switch msg.Role {
		case port.RoleSystem:
			text, err := contentPartsToTextOnlyString(msg.ContentParts, "claude", model, "system")
			if err != nil {
				return nil, nil, err
			}
			system = append(system, anthropic.TextBlockParam{Text: text})
		case port.RoleUser:
			blocks, err := toAnthropicUserBlocks(msg.ContentParts, model)
			if err != nil {
				return nil, nil, err
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		case port.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			content, err := contentPartsToTextOnlyString(msg.ContentParts, "claude", model, "assistant")
			if err != nil {
				return nil, nil, err
			}
			if content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(content))
			}
			for _, tc := range msg.ToolCalls {
				var input any
				_ = json.Unmarshal(tc.Arguments, &input)
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		case port.RoleTool:
			var blocks []anthropic.ContentBlockParamUnion
			for _, tr := range msg.ToolResults {
				contentBlocks, err := toAnthropicToolResultBlocks(tr.ContentParts, model)
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

func toAnthropicTools(tools []port.ToolSpec) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		var schema anthropic.ToolInputSchemaParam
		_ = json.Unmarshal(t.InputSchema, &schema)

		tp := anthropic.ToolParam{
			Name:        t.Name,
			InputSchema: schema,
		}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		result[i] = anthropic.ToolUnionParam{OfTool: &tp}
	}
	return result
}

// ─── 响应映射 ────────────────────────────────────────

func fromAnthropicResponse(msg *anthropic.Message) *port.CompletionResponse {
	var contentParts []port.ContentPart
	var toolCalls []port.ToolCall

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if strings.TrimSpace(v.Text) != "" {
				contentParts = append(contentParts, port.TextPart(v.Text))
			}
		case anthropic.ToolUseBlock:
			toolCalls = append(toolCalls, port.ToolCall{
				ID:        v.ID,
				Name:      v.Name,
				Arguments: v.Input,
			})
		}
	}

	return &port.CompletionResponse{
		Message: port.Message{
			Role:         port.RoleAssistant,
			ContentParts: contentParts,
			ToolCalls:    toolCalls,
		},
		ToolCalls: toolCalls,
		Usage: port.TokenUsage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
		StopReason: string(msg.StopReason),
	}
}

func toAnthropicUserBlocks(parts []port.ContentPart, model string) ([]anthropic.ContentBlockParamUnion, error) {
	result := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case port.ContentPartText:
			result = append(result, anthropic.NewTextBlock(part.Text))
		case port.ContentPartInputImage:
			block, err := toAnthropicImageBlock(part)
			if err != nil {
				return nil, err
			}
			result = append(result, block)
		default:
			return nil, unsupportedPartError("claude", model, "user", part.Type)
		}
	}
	return result, nil
}

func toAnthropicToolResultBlocks(parts []port.ContentPart, model string) ([]anthropic.ToolResultBlockParamContentUnion, error) {
	result := make([]anthropic.ToolResultBlockParamContentUnion, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case port.ContentPartText:
			result = append(result, anthropic.ToolResultBlockParamContentUnion{
				OfText: &anthropic.TextBlockParam{Text: part.Text},
			})
		case port.ContentPartOutputImage:
			block, err := toAnthropicImageBlock(part)
			if err != nil {
				return nil, err
			}
			result = append(result, anthropic.ToolResultBlockParamContentUnion{
				OfImage: block.OfImage,
			})
		default:
			return nil, unsupportedPartError("claude", model, "tool_result", part.Type)
		}
	}
	return result, nil
}

func toAnthropicImageBlock(part port.ContentPart) (anthropic.ContentBlockParamUnion, error) {
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

func contentPartsToTextOnlyString(parts []port.ContentPart, provider, model, role string) (string, error) {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type != port.ContentPartText {
			return "", unsupportedPartError(provider, model, role, part.Type)
		}
		textParts = append(textParts, part.Text)
	}
	return strings.Join(textParts, "\n"), nil
}

func unsupportedPartError(provider, model, role string, typ port.ContentPartType) error {
	return fmt.Errorf("%s adapter: model=%q role=%s unsupported content part type=%q", provider, model, role, typ)
}

// ─── 流式迭代器 ──────────────────────────────────────

type toolUseBuilder struct {
	id    string
	name  string
	input string
}

type streamIterator struct {
	stream          *ssestream.Stream[anthropic.MessageStreamEventUnion]
	pending         []port.StreamChunk
	done            bool
	usage           port.TokenUsage
	toolUseBuilders map[int]*toolUseBuilder
}

func (it *streamIterator) Next() (port.StreamChunk, error) {
	for {
		if len(it.pending) > 0 {
			chunk := it.pending[0]
			it.pending = it.pending[1:]
			return chunk, nil
		}
		if it.done {
			return port.StreamChunk{}, io.EOF
		}
		if !it.stream.Next() {
			if err := it.stream.Err(); err != nil {
				return port.StreamChunk{}, err
			}
			it.done = true
			return port.StreamChunk{}, io.EOF
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
			it.pending = append(it.pending, port.StreamChunk{Delta: d.Text})
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
			tc := port.ToolCall{
				ID:        tb.id,
				Name:      tb.name,
				Arguments: json.RawMessage(input),
			}
			it.pending = append(it.pending, port.StreamChunk{ToolCall: &tc})
			delete(it.toolUseBuilders, int(e.Index))
		}

	case anthropic.MessageDeltaEvent:
		it.usage.CompletionTokens = int(e.Usage.OutputTokens)
		it.usage.TotalTokens = it.usage.PromptTokens + it.usage.CompletionTokens

	case anthropic.MessageStopEvent:
		it.pending = append(it.pending, port.StreamChunk{
			Done:  true,
			Usage: &it.usage,
		})
	}
}

func (it *streamIterator) Close() error {
	it.done = true
	return nil
}
