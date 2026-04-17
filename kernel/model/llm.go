package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"strings"
)

// LLM 是统一的模型生成接口。
// GenerateContent 返回一个流式迭代器，依次产出 StreamChunk。
// 非流式实现产出单个 Done=true 的 chunk；流式实现逐 chunk 产出。
type LLM interface {
	GenerateContent(ctx context.Context, req CompletionRequest) iter.Seq2[StreamChunk, error]
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
	Model                 string           `json:"model"`
	MaxTokens             int              `json:"max_tokens,omitempty"`
	Temperature           float64          `json:"temperature,omitempty"`
	ContextWindow         int              `json:"context_window,omitempty"`
	AutoCompactTokenLimit int              `json:"auto_compact_token_limit,omitempty"`
	Extra                 map[string]any   `json:"extra,omitempty"`
	Requirements          *TaskRequirement `json:"requirements,omitempty"`
}

// LLMCallAttempt 描述一次候选模型尝试的结果，用于 failover 观测。
type LLMCallAttempt struct {
	CandidateModel string `json:"candidate_model,omitempty"`
	AttemptIndex   int    `json:"attempt_index,omitempty"`
	CandidateRetry int    `json:"candidate_retry,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	BreakerState   string `json:"breaker_state,omitempty"`
	FailoverTo     string `json:"failover_to,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
}

// LLMCallMetadata 记录一次 LLM 调用的实际命中模型和 failover 尝试细节。
type LLMCallMetadata struct {
	ActualModel string           `json:"actual_model,omitempty"`
	Attempts    []LLMCallAttempt `json:"attempts,omitempty"`
}

type LLMErrorClass string

const (
	LLMErrorUnknown       LLMErrorClass = "unknown"
	LLMErrorTimeout       LLMErrorClass = "timeout"
	LLMErrorRateLimit     LLMErrorClass = "rate_limit"
	LLMErrorContextWindow LLMErrorClass = "context_window"
	LLMErrorAuth          LLMErrorClass = "auth"
	LLMErrorUnavailable   LLMErrorClass = "unavailable"
	LLMErrorInvalid       LLMErrorClass = "invalid_request"
	LLMErrorCancelled     LLMErrorClass = "cancelled"
)

// CompletionResponse 是 LLM 返回的同步响应。
type CompletionResponse struct {
	Message    Message          `json:"message"`
	ToolCalls  []ToolCall       `json:"tool_calls,omitempty"`
	Usage      TokenUsage       `json:"usage"`
	StopReason string           `json:"stop_reason"`
	Metadata   *LLMCallMetadata `json:"metadata,omitempty"`
}

// LLMCallError 为 LLM 调用错误附加重试、fallback 和观测元数据。
type LLMCallError struct {
	Err          error
	Class        LLMErrorClass
	Retryable    bool
	FallbackSafe bool
	Metadata     LLMCallMetadata
}

func (e *LLMCallError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *LLMCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ClassifyError(err error) LLMErrorClass {
	if err == nil {
		return LLMErrorUnknown
	}
	var callErr *LLMCallError
	if errors.As(err, &callErr) && callErr != nil && callErr.Class != "" {
		return callErr.Class
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled), strings.Contains(msg, "context canceled"):
		return LLMErrorCancelled
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return LLMErrorTimeout
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"), strings.Contains(msg, "429"):
		return LLMErrorRateLimit
	case strings.Contains(msg, "context window"), strings.Contains(msg, "context_length_exceeded"), strings.Contains(msg, "maximum context length"), strings.Contains(msg, "prompt is too long"), strings.Contains(msg, "too many tokens"), strings.Contains(msg, "input token"):
		return LLMErrorContextWindow
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "invalid api key"), strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return LLMErrorAuth
	case strings.Contains(msg, "bad request"), strings.Contains(msg, "invalid request"), strings.Contains(msg, "malformed"):
		return LLMErrorInvalid
	case strings.Contains(msg, "unavailable"), strings.Contains(msg, "overloaded"), strings.Contains(msg, "service down"), strings.Contains(msg, "502"), strings.Contains(msg, "503"), strings.Contains(msg, "504"):
		return LLMErrorUnavailable
	default:
		return LLMErrorUnknown
	}
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

type StreamChunkType string

const (
	StreamChunkTextDelta      StreamChunkType = "text.delta"
	StreamChunkReasoningDelta StreamChunkType = "reasoning.delta"
	StreamChunkRefusalDelta   StreamChunkType = "refusal.delta"
	StreamChunkToolCall       StreamChunkType = "tool.call"
	StreamChunkHostedTool     StreamChunkType = "hosted_tool"
	StreamChunkDone           StreamChunkType = "done"
)

// StreamChunk 是流式响应的一个片段。
type StreamChunk struct {
	Type           StreamChunkType  `json:"type,omitempty"`
	Content        string           `json:"content,omitempty"`
	ToolCall       *ToolCall        `json:"tool_call,omitempty"`
	HostedTool     *HostedToolEvent `json:"hosted_tool,omitempty"`
	Usage          *TokenUsage      `json:"usage,omitempty"`
	StopReason     string           `json:"stop_reason,omitempty"`
	Metadata       *LLMCallMetadata `json:"metadata,omitempty"`
	Delta          string           `json:"delta,omitempty"`
	ReasoningDelta string           `json:"reasoning_delta,omitempty"`
	Done           bool             `json:"done,omitempty"`
}

func TextDeltaChunk(content string) StreamChunk {
	return StreamChunk{Type: StreamChunkTextDelta, Content: content, Delta: content}
}

func ReasoningDeltaChunk(content string) StreamChunk {
	return StreamChunk{Type: StreamChunkReasoningDelta, Content: content, ReasoningDelta: content}
}

func RefusalDeltaChunk(content string) StreamChunk {
	return StreamChunk{Type: StreamChunkRefusalDelta, Content: content}
}

func ToolCallChunk(call *ToolCall) StreamChunk {
	return StreamChunk{Type: StreamChunkToolCall, ToolCall: call}
}

func HostedToolChunk(event *HostedToolEvent) StreamChunk {
	return StreamChunk{Type: StreamChunkHostedTool, HostedTool: event}
}

func DoneChunk(stopReason string, usage *TokenUsage, metadata *LLMCallMetadata) StreamChunk {
	return StreamChunk{Type: StreamChunkDone, Usage: usage, StopReason: stopReason, Metadata: metadata, Done: true}
}

func (c StreamChunk) Normalized() StreamChunk {
	if c.Type != "" {
		if c.Type == StreamChunkTextDelta && c.Content == "" {
			c.Content = c.Delta
		}
		if c.Type == StreamChunkReasoningDelta && c.Content == "" {
			c.Content = c.ReasoningDelta
		}
		if c.Type == StreamChunkDone {
			c.Done = true
		}
		return c
	}
	switch {
	case c.ReasoningDelta != "":
		c.Type = StreamChunkReasoningDelta
		c.Content = c.ReasoningDelta
	case c.Delta != "":
		c.Type = StreamChunkTextDelta
		c.Content = c.Delta
	case c.ToolCall != nil:
		c.Type = StreamChunkToolCall
	case c.HostedTool != nil:
		c.Type = StreamChunkHostedTool
	case c.Done:
		c.Type = StreamChunkDone
	}
	return c
}

func (c StreamChunk) IsDone() bool {
	return c.Normalized().Type == StreamChunkDone
}

func (c StreamChunk) EmitsVisibleContent() bool {
	switch c.Normalized().Type {
	case StreamChunkTextDelta, StreamChunkReasoningDelta, StreamChunkRefusalDelta, StreamChunkToolCall, StreamChunkHostedTool:
		return true
	default:
		return false
	}
}

// ──────────────────────────────────────────────────────────────
// 便利函数：将统一的 iter.Seq2 接口适配为同步或旧式迭代器消费。
// ──────────────────────────────────────────────────────────────

// Complete 调用 GenerateContent 并将流式 chunk 累积为单个 CompletionResponse。
// 适用于不需要流式处理的消费方（摘要、评估、上下文压缩等）。
func Complete(ctx context.Context, llm LLM, req CompletionRequest) (*CompletionResponse, error) {
	var (
		content         strings.Builder
		reasoning       strings.Builder
		refusal         strings.Builder
		toolCalls       []ToolCall
		hostedToolCalls []HostedToolEvent
		hostedToolIndex = map[string]int{}
		usage           TokenUsage
		stopReason      string
		metadata        *LLMCallMetadata
	)
	for chunk, err := range llm.GenerateContent(ctx, req) {
		if err != nil {
			return nil, err
		}
		chunk = chunk.Normalized()
		switch chunk.Type {
		case StreamChunkTextDelta:
			content.WriteString(chunk.Content)
		case StreamChunkReasoningDelta:
			reasoning.WriteString(chunk.Content)
		case StreamChunkRefusalDelta:
			refusal.WriteString(chunk.Content)
		case StreamChunkToolCall:
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
			}
		case StreamChunkHostedTool:
			if chunk.HostedTool != nil {
				hostedToolCalls = upsertHostedToolCalls(hostedToolCalls, hostedToolIndex, *chunk.HostedTool)
			}
		}
		if chunk.Metadata != nil {
			metadata = chunk.Metadata
		}
		if chunk.IsDone() {
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			stopReason = strings.TrimSpace(chunk.StopReason)
		}
	}

	msg := Message{Role: RoleAssistant, ToolCalls: toolCalls, HostedToolCalls: hostedToolCalls}
	if reasoning.Len() > 0 {
		msg.ContentParts = append(msg.ContentParts, ReasoningPart(reasoning.String()))
	}
	if refusal.Len() > 0 {
		msg.ContentParts = append(msg.ContentParts, RefusalPart(refusal.String()))
	}
	if content.Len() > 0 {
		msg.ContentParts = append(msg.ContentParts, TextPart(content.String()))
	}
	if stopReason == "" {
		if len(toolCalls) > 0 {
			stopReason = "tool_use"
		} else {
			stopReason = "end_turn"
		}
	}

	return &CompletionResponse{
		Message:    msg,
		ToolCalls:  toolCalls,
		Usage:      usage,
		StopReason: stopReason,
		Metadata:   metadata,
	}, nil
}

// ResponseToSeq 将 CompletionResponse 转换为 iter.Seq2（单 chunk 流）。
func ResponseToSeq(resp *CompletionResponse) iter.Seq2[StreamChunk, error] {
	return func(yield func(StreamChunk, error) bool) {
		if resp == nil {
			return
		}
		// Yield reasoning part if present.
		if reasoning := ContentPartsToReasoningText(resp.Message.ContentParts); reasoning != "" {
			if !yield(ReasoningDeltaChunk(reasoning), nil) {
				return
			}
		}
		if refusal := ContentPartsToRefusalText(resp.Message.ContentParts); refusal != "" {
			if !yield(RefusalDeltaChunk(refusal), nil) {
				return
			}
		}
		for i := range resp.Message.HostedToolCalls {
			event := resp.Message.HostedToolCalls[i]
			if !yield(HostedToolChunk(&event), nil) {
				return
			}
		}
		// Yield tool calls.
		for i := range resp.ToolCalls {
			call := resp.ToolCalls[i]
			if !yield(ToolCallChunk(&call), nil) {
				return
			}
		}
		// Yield content + done.
		content := ContentPartsToTextOnly(resp.Message.ContentParts)
		var meta *LLMCallMetadata
		if resp.Metadata != nil {
			copied := *resp.Metadata
			meta = &copied
		}
		if content != "" {
			if !yield(TextDeltaChunk(content), nil) {
				return
			}
		}
		yield(DoneChunk(resp.StopReason, &resp.Usage, meta), nil)
	}
}

func upsertHostedToolCalls(existing []HostedToolEvent, index map[string]int, event HostedToolEvent) []HostedToolEvent {
	key := strings.TrimSpace(event.ID)
	if key == "" {
		key = strings.TrimSpace(event.Name)
	}
	if key == "" {
		key = string(event.Input) + "\x00" + string(event.Output)
	}
	if pos, ok := index[key]; ok {
		existing[pos] = event
		return existing
	}
	index[key] = len(existing)
	return append(existing, event)
}

// SeqToIterator 将 iter.Seq2（push 模式）转换为 StreamIterator（pull 模式）。
// 内部使用 iter.Pull2 桥接。调用方必须调用返回的 StreamIterator.Close() 释放资源。
func SeqToIterator(seq iter.Seq2[StreamChunk, error]) StreamIterator {
	next, stop := iter.Pull2(seq)
	return &seqIterator{next: next, stop: stop}
}

type seqIterator struct {
	next func() (StreamChunk, error, bool)
	stop func()
}

func (it *seqIterator) Next() (StreamChunk, error) {
	chunk, err, ok := it.next()
	if !ok {
		return StreamChunk{}, io.EOF
	}
	return chunk, err
}

func (it *seqIterator) Close() error {
	it.stop()
	return nil
}
