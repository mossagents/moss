package claude

import (
	"context"
	"encoding/json"
	"io"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/mossagi/moss/kernel/port"
)

// 确保实现 port.LLM 和 port.StreamingLLM 接口。
var (
	_ port.LLM         = (*Client)(nil)
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

// Complete 实现 port.LLM（同步模式）。
func (c *Client) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	msg, err := c.client.Messages.New(ctx, c.buildParams(req))
	if err != nil {
		return nil, err
	}
	return fromAnthropicResponse(msg), nil
}

// Stream 实现 port.StreamingLLM（流式模式）。
func (c *Client) Stream(ctx context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
	stream := c.client.Messages.NewStreaming(ctx, c.buildParams(req))
	return &streamIterator{
		stream:          stream,
		toolUseBuilders: make(map[int]*toolUseBuilder),
	}, nil
}

// buildParams 构建 Anthropic API 请求参数。
func (c *Client) buildParams(req port.CompletionRequest) anthropic.MessageNewParams {
	model := c.model
	maxTokens := c.maxTokens
	if req.Config.Model != "" {
		model = req.Config.Model
	}
	if req.Config.MaxTokens > 0 {
		maxTokens = int64(req.Config.MaxTokens)
	}

	system, messages := toAnthropicMessages(req.Messages)
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
	return params
}

// ─── 消息映射 ────────────────────────────────────────

func toAnthropicMessages(msgs []port.Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var system []anthropic.TextBlockParam
	var messages []anthropic.MessageParam

	for _, msg := range msgs {
		switch msg.Role {
		case port.RoleSystem:
			system = append(system, anthropic.TextBlockParam{Text: msg.Content})
		case port.RoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case port.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
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
				blocks = append(blocks, anthropic.NewToolResultBlock(tr.CallID, tr.Content, tr.IsError))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewUserMessage(blocks...))
			}
		}
	}
	return system, messages
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
	var content string
	var toolCalls []port.ToolCall

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			content += v.Text
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
			Role:      port.RoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
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
