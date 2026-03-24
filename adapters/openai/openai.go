package openai

import (
	"context"
	"encoding/json"
	"io"

	"github.com/mossagi/moss/kernel/port"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"
)

// 确保实现 port.LLM 和 port.StreamingLLM 接口。
var (
	_ port.LLM          = (*Client)(nil)
	_ port.StreamingLLM = (*Client)(nil)
)

const DefaultModel = "gpt-4o"

// Client 是 OpenAI Chat Completion 适配器。
type Client struct {
	client    openai.Client
	model     string
	maxTokens int64
}

// Option 是 Client 的配置选项。
type Option func(*Client)

// WithModel 设置默认模型。
func WithModel(model string) Option { return func(c *Client) { c.model = model } }

// WithMaxTokens 设置默认最大 token 数。
func WithMaxTokens(n int64) Option { return func(c *Client) { c.maxTokens = n } }

// New 创建 OpenAI 适配器。apiKey 为空时从 OPENAI_API_KEY 环境变量读取。
// 可通过 extraOpts 传入 option.WithBaseURL 等实现兼容 API 调用。
func New(apiKey string, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	c := &Client{
		client:    openai.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 4096,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewWithBaseURL 创建 OpenAI 兼容适配器，允许指定 API Key 和 Base URL。
func NewWithBaseURL(apiKey, baseURL string, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(baseURL))
	}
	return NewWithRequestOptions(reqOpts, opts...)
}

// NewWithRequestOptions 创建 OpenAI 适配器，允许传入 option.RequestOption（如 WithBaseURL）。
func NewWithRequestOptions(reqOpts []option.RequestOption, opts ...Option) *Client {
	c := &Client{
		client:    openai.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 4096,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Complete 实现 port.LLM（同步模式）。
func (c *Client) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	completion, err := c.client.Chat.Completions.New(ctx, c.buildParams(req))
	if err != nil {
		return nil, err
	}
	return fromOpenAIResponse(completion), nil
}

// Stream 实现 port.StreamingLLM（流式模式）。
func (c *Client) Stream(ctx context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
	stream := c.client.Chat.Completions.NewStreaming(ctx, c.buildParams(req))
	return &streamIterator{
		stream:       stream,
		toolBuilders: make(map[int]*toolCallBuilder),
	}, nil
}

// ─── 请求构建 ────────────────────────────────────────

func (c *Client) buildParams(req port.CompletionRequest) openai.ChatCompletionNewParams {
	model := c.model
	if req.Config.Model != "" {
		model = req.Config.Model
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: toOpenAIMessages(req.Messages),
	}

	maxTokens := c.maxTokens
	if req.Config.MaxTokens > 0 {
		maxTokens = int64(req.Config.MaxTokens)
	}
	if maxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(maxTokens)
	}

	if req.Config.Temperature > 0 {
		params.Temperature = openai.Float(req.Config.Temperature)
	}

	if tools := toOpenAITools(req.Tools); len(tools) > 0 {
		params.Tools = tools
	}

	// ResponseFormat 支持
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
			}
		case "json_schema":
			if req.ResponseFormat.JSONSchema != nil {
				var schema interface{}
				if req.ResponseFormat.JSONSchema.Schema != nil {
					_ = json.Unmarshal(req.ResponseFormat.JSONSchema.Schema, &schema)
				}
				params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
						JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
							Name:   req.ResponseFormat.JSONSchema.Name,
							Schema: schema,
							Strict: openai.Bool(req.ResponseFormat.JSONSchema.Strict),
						},
					},
				}
			}
		}
	}

	return params
}

// ─── 消息映射 ────────────────────────────────────────

func toOpenAIMessages(msgs []port.Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion

	for _, msg := range msgs {
		switch msg.Role {
		case port.RoleSystem:
			result = append(result, openai.SystemMessage(msg.Content))

		case port.RoleUser:
			result = append(result, openai.UserMessage(msg.Content))

		case port.RoleAssistant:
			param := toAssistantMessage(msg)
			if param != nil {
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: param})
			}

		case port.RoleTool:
			for _, tr := range msg.ToolResults {
				result = append(result, openai.ToolMessage(tr.Content, tr.CallID))
			}
		}
	}
	return result
}

func toAssistantMessage(msg port.Message) *openai.ChatCompletionAssistantMessageParam {
	if msg.Content == "" && len(msg.ToolCalls) == 0 {
		return nil
	}
	p := &openai.ChatCompletionAssistantMessageParam{}
	if msg.Content != "" {
		p.Content.OfString = openai.String(msg.Content)
	}
	if len(msg.ToolCalls) > 0 {
		p.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			p.ToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
				ID: tc.ID,
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				},
			}
		}
	}
	return p
}

// ─── 工具映射 ────────────────────────────────────────

func toOpenAITools(tools []port.ToolSpec) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openai.ChatCompletionToolParam, len(tools))
	for i, t := range tools {
		var params shared.FunctionParameters
		if len(t.InputSchema) > 0 {
			_ = json.Unmarshal(t.InputSchema, &params)
		}
		def := shared.FunctionDefinitionParam{
			Name:       t.Name,
			Parameters: params,
		}
		if t.Description != "" {
			def.Description = openai.String(t.Description)
		}
		result[i] = openai.ChatCompletionToolParam{
			Function: def,
		}
	}
	return result
}

// ─── 响应映射 ────────────────────────────────────────

func fromOpenAIResponse(c *openai.ChatCompletion) *port.CompletionResponse {
	if len(c.Choices) == 0 {
		return &port.CompletionResponse{
			Message: port.Message{Role: port.RoleAssistant},
			Usage:   fromUsage(c.Usage),
		}
	}

	choice := c.Choices[0]
	toolCalls := fromToolCalls(choice.Message.ToolCalls)

	return &port.CompletionResponse{
		Message: port.Message{
			Role:      port.RoleAssistant,
			Content:   choice.Message.Content,
			ToolCalls: toolCalls,
		},
		ToolCalls:  toolCalls,
		Usage:      fromUsage(c.Usage),
		StopReason: choice.FinishReason,
	}
}

func fromToolCalls(tcs []openai.ChatCompletionMessageToolCall) []port.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	result := make([]port.ToolCall, len(tcs))
	for i, tc := range tcs {
		result[i] = port.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		}
	}
	return result
}

func fromUsage(u openai.CompletionUsage) port.TokenUsage {
	return port.TokenUsage{
		PromptTokens:     int(u.PromptTokens),
		CompletionTokens: int(u.CompletionTokens),
		TotalTokens:      int(u.TotalTokens),
	}
}

// ─── 流式迭代器 ──────────────────────────────────────

type toolCallBuilder struct {
	id        string
	name      string
	arguments string
}

type streamIterator struct {
	stream       *ssestream.Stream[openai.ChatCompletionChunk]
	pending      []port.StreamChunk
	done         bool
	usage        port.TokenUsage
	toolBuilders map[int]*toolCallBuilder
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
			// 流结束，发出已累积的工具调用和完成信号
			it.flushToolCalls()
			it.pending = append(it.pending, port.StreamChunk{
				Done:  true,
				Usage: &it.usage,
			})
			it.done = true
			continue
		}
		it.processChunk(it.stream.Current())
	}
}

func (it *streamIterator) processChunk(chunk openai.ChatCompletionChunk) {
	// 更新 usage（仅最后一个 chunk 非零）
	if chunk.Usage.TotalTokens > 0 {
		it.usage = port.TokenUsage{
			PromptTokens:     int(chunk.Usage.PromptTokens),
			CompletionTokens: int(chunk.Usage.CompletionTokens),
			TotalTokens:      int(chunk.Usage.TotalTokens),
		}
	}

	if len(chunk.Choices) == 0 {
		return
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// 文本增量
	if delta.Content != "" {
		it.pending = append(it.pending, port.StreamChunk{Delta: delta.Content})
	}

	// 工具调用增量
	for _, tc := range delta.ToolCalls {
		idx := int(tc.Index)
		if _, ok := it.toolBuilders[idx]; !ok {
			it.toolBuilders[idx] = &toolCallBuilder{}
		}
		tb := it.toolBuilders[idx]
		if tc.ID != "" {
			tb.id = tc.ID
		}
		if tc.Function.Name != "" {
			tb.name = tc.Function.Name
		}
		tb.arguments += tc.Function.Arguments
	}

	// 如果 finish_reason 是 tool_calls 或 stop，flush 工具调用
	if choice.FinishReason == "tool_calls" {
		it.flushToolCalls()
	}
}

func (it *streamIterator) flushToolCalls() {
	for idx, tb := range it.toolBuilders {
		args := tb.arguments
		if args == "" {
			args = "{}"
		}
		tc := port.ToolCall{
			ID:        tb.id,
			Name:      tb.name,
			Arguments: json.RawMessage(args),
		}
		it.pending = append(it.pending, port.StreamChunk{ToolCall: &tc})
		delete(it.toolBuilders, idx)
	}
}

func (it *streamIterator) Close() error {
	it.done = true
	return it.stream.Close()
}
