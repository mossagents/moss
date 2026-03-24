package port

import (
	"context"
	"encoding/json"
	"io"
)

// LLM 是模型调用的核心接口（同步模式）。
type LLM interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

// StreamingLLM 是流式模型调用接口（可选实现）。
type StreamingLLM interface {
	Stream(ctx context.Context, req CompletionRequest) (StreamIterator, error)
}

// CompletionRequest 是发送给 LLM 的请求。
type CompletionRequest struct {
	Messages       []Message       `json:"messages"`
	Tools          []ToolSpec      `json:"tools,omitempty"`
	Config         ModelConfig     `json:"config"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ResponseFormat 控制 LLM 的输出格式。
type ResponseFormat struct {
	// Type 指定输出格式类型：
	//   - "text": 默认自由文本
	//   - "json_object": 强制 JSON 输出
	//   - "json_schema": 按指定 JSON Schema 输出
	Type string `json:"type"`

	// JSONSchema 当 Type="json_schema" 时，描述期望的输出结构。
	JSONSchema *JSONSchemaSpec `json:"json_schema,omitempty"`
}

// JSONSchemaSpec 描述 JSON Schema 约束。
type JSONSchemaSpec struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

// ModelConfig 配置模型参数。
type ModelConfig struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

// CompletionResponse 是 LLM 返回的同步响应。
type CompletionResponse struct {
	Message    Message    `json:"message"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stop_reason"`
}

// ToolSpec 描述一个工具的声明信息，供 LLM 选择调用。
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// StreamIterator 是流式响应的迭代器。
type StreamIterator interface {
	// Next 返回下一个 chunk，流结束时返回 io.EOF。
	Next() (StreamChunk, error)
	// Close 释放迭代器资源。
	Close() error
}

// StreamChunk 是流式响应的一个片段。
type StreamChunk struct {
	Delta    string      `json:"delta,omitempty"`
	ToolCall *ToolCall   `json:"tool_call,omitempty"`
	Done     bool        `json:"done,omitempty"`
	Usage    *TokenUsage `json:"usage,omitempty"`
}

// 确保 io.EOF 可用于 StreamIterator.Next 的终止判断。
var _ error = io.EOF
