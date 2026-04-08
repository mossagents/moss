package openai

import (
	"context"
	"encoding/json"
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// 确保实现 mdl.LLM 和 mdl.StreamingLLM 接口。
var (
	_ mdl.LLM          = (*Client)(nil)
	_ mdl.StreamingLLM = (*Client)(nil)
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

// NewWithHTTPClient creates an OpenAI adapter using a custom http.Client.
// Use this to inject a tracing transport for distributed trace propagation:
//
//	transport := &mossotel.TraceTransport{Base: http.DefaultTransport}
//	adapter := openai.NewWithHTTPClient(apiKey, &http.Client{Transport: transport})
func NewWithHTTPClient(apiKey string, httpClient *http.Client, opts ...Option) *Client {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		reqOpts = append(reqOpts, option.WithHTTPClient(httpClient))
	}
	return NewWithRequestOptions(reqOpts, opts...)
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

// Complete 实现 mdl.LLM（同步模式）。
func (c *Client) Complete(ctx context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return nil, err
	}
	completion, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return fromOpenAIResponse(completion), nil
}

// Stream 实现 mdl.StreamingLLM（流式模式）。
func (c *Client) Stream(ctx context.Context, req mdl.CompletionRequest) (mdl.StreamIterator, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return nil, err
	}
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	return &streamIterator{
		stream:       stream,
		toolBuilders: make(map[int]*toolCallBuilder),
	}, nil
}

// ─── 请求构建 ────────────────────────────────────────

func (c *Client) buildParams(req mdl.CompletionRequest) (openai.ChatCompletionNewParams, error) {
	model := c.model
	if req.Config.Model != "" {
		model = req.Config.Model
	}

	messages, err := toOpenAIMessages(req.Messages, model)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: messages,
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

	return params, nil
}

// ─── 消息映射 ────────────────────────────────────────

// normalizeMessages merges consecutive messages with the same role, excluding
// system and tool messages, to prevent API-level validation errors from strict
// providers such as DeepSeek that forbid consecutive same-role turns.
func normalizeMessages(msgs []mdl.Message) []mdl.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]mdl.Message, 0, len(msgs))
	for _, msg := range msgs {
		if len(out) > 0 &&
			msg.Role != mdl.RoleSystem &&
			msg.Role != mdl.RoleTool &&
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

func toOpenAIMessages(msgs []mdl.Message, model string) ([]openai.ChatCompletionMessageParamUnion, error) {
	msgs = normalizeMessages(msgs)
	var result []openai.ChatCompletionMessageParamUnion

	for _, msg := range msgs {
		switch msg.Role {
		case mdl.RoleSystem:
			parts, err := toOpenAISystemTextParts(msg.ContentParts, model)
			if err != nil {
				return nil, err
			}
			result = append(result, openai.SystemMessage(parts))

		case mdl.RoleUser:
			parts, err := toOpenAIUserParts(msg.ContentParts, model)
			if err != nil {
				return nil, err
			}
			result = append(result, openai.UserMessage(parts))

		case mdl.RoleAssistant:
			param, err := toAssistantMessage(msg, model)
			if err != nil {
				return nil, err
			}
			if param != nil {
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: param})
			}

		case mdl.RoleTool:
			for _, tr := range msg.ToolResults {
				content, err := contentPartsToTextOnlyString(tr.ContentParts, "openai", model, "tool_result")
				if err != nil {
					return nil, err
				}
				result = append(result, openai.ToolMessage(content, tr.CallID))
			}
		}
	}
	return result, nil
}

func toOpenAISystemTextParts(parts []mdl.ContentPart, model string) ([]openai.ChatCompletionContentPartTextParam, error) {
	result := make([]openai.ChatCompletionContentPartTextParam, 0, len(parts))
	for _, part := range parts {
		if part.Type != mdl.ContentPartText {
			return nil, unsupportedPartError("openai", model, "system", part.Type)
		}
		result = append(result, openai.ChatCompletionContentPartTextParam{Text: part.Text})
	}
	return result, nil
}

func toOpenAIUserParts(parts []mdl.ContentPart, model string) ([]openai.ChatCompletionContentPartUnionParam, error) {
	result := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case mdl.ContentPartText:
			result = append(result, openai.TextContentPart(part.Text))
		case mdl.ContentPartInputImage:
			imageURL := part.URL
			if strings.TrimSpace(imageURL) == "" {
				imageURL = "data:" + part.MIMEType + ";base64," + part.DataBase64
			}
			result = append(result, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: imageURL,
			}))
		case mdl.ContentPartInputAudio:
			audioPart, err := toOpenAIInputAudioPart(part, model)
			if err != nil {
				return nil, err
			}
			result = append(result, audioPart)
		case mdl.ContentPartInputVideo:
			videoPart, err := toOpenAIInputVideoPart(part, model)
			if err != nil {
				return nil, err
			}
			result = append(result, videoPart)
		default:
			return nil, unsupportedPartError("openai", model, "user", part.Type)
		}
	}
	return result, nil
}

func toOpenAIInputAudioPart(part mdl.ContentPart, model string) (openai.ChatCompletionContentPartUnionParam, error) {
	if strings.TrimSpace(part.URL) != "" {
		return openai.ChatCompletionContentPartUnionParam{}, capabilityUnavailableError("openai", model, "user", part.Type, "url source is not supported for audio input")
	}
	format, err := audioFormatFromMIME(part.MIMEType)
	if err != nil {
		return openai.ChatCompletionContentPartUnionParam{}, capabilityUnavailableError("openai", model, "user", part.Type, err.Error())
	}
	return openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
		Data:   part.DataBase64,
		Format: format,
	}), nil
}

func toOpenAIInputVideoPart(part mdl.ContentPart, model string) (openai.ChatCompletionContentPartUnionParam, error) {
	if strings.TrimSpace(part.URL) != "" {
		return openai.ChatCompletionContentPartUnionParam{}, capabilityUnavailableError("openai", model, "user", part.Type, "url source is not supported for video input")
	}
	filename := strings.TrimSpace(filepath.Base(part.SourcePath))
	if filename == "" || filename == "." {
		filename = "input-video.bin"
	}
	return openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
		FileData: openai.String(part.DataBase64),
		Filename: openai.String(filename),
	}), nil
}

func audioFormatFromMIME(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav", nil
	case "audio/mp3", "audio/mpeg":
		return "mp3", nil
	default:
		return "", fmt.Errorf("unsupported audio mime_type=%q for OpenAI input_audio", mimeType)
	}
}

func toAssistantMessage(msg mdl.Message, model string) (*openai.ChatCompletionAssistantMessageParam, error) {
	content, err := contentPartsToTextOnlyString(msg.ContentParts, "openai", model, "assistant")
	if err != nil {
		return nil, err
	}
	if content == "" && len(msg.ToolCalls) == 0 {
		return nil, nil
	}
	p := &openai.ChatCompletionAssistantMessageParam{}
	if content != "" {
		p.Content.OfString = openai.String(content)
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
	return p, nil
}

// ─── 工具映射 ────────────────────────────────────────

func toOpenAITools(tools []mdl.ToolSpec) []openai.ChatCompletionToolParam {
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

func fromOpenAIResponse(c *openai.ChatCompletion) *mdl.CompletionResponse {
	if len(c.Choices) == 0 {
		return &mdl.CompletionResponse{
			Message: mdl.Message{Role: mdl.RoleAssistant},
			Usage:   fromUsage(c.Usage),
		}
	}

	choice := c.Choices[0]
	toolCalls := fromToolCalls(choice.Message.ToolCalls)
	var contentParts []mdl.ContentPart
	if reasoning := extractReasoningText(choice.Message.RawJSON()); strings.TrimSpace(reasoning) != "" {
		contentParts = append(contentParts, mdl.ReasoningPart(reasoning))
	}
	if choice.Message.Content != "" {
		contentParts = append(contentParts, mdl.TextPart(choice.Message.Content))
	}
	if strings.TrimSpace(choice.Message.Audio.Data) != "" {
		contentParts = append(contentParts, mdl.MediaInlinePart(
			mdl.ContentPartOutputAudio,
			"audio/wav",
			choice.Message.Audio.Data,
			"",
		))
	}

	return &mdl.CompletionResponse{
		Message: mdl.Message{
			Role:         mdl.RoleAssistant,
			ContentParts: contentParts,
			ToolCalls:    toolCalls,
		},
		ToolCalls:  toolCalls,
		Usage:      fromUsage(c.Usage),
		StopReason: normalizeOpenAIStopReason(choice.FinishReason),
	}
}

func normalizeOpenAIStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

func contentPartsToTextOnlyString(parts []mdl.ContentPart, provider, model, role string) (string, error) {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if role == "assistant" && part.Type == mdl.ContentPartReasoning {
			continue
		}
		if part.Type != mdl.ContentPartText {
			return "", unsupportedPartError(provider, model, role, part.Type)
		}
		textParts = append(textParts, part.Text)
	}
	return strings.Join(textParts, "\n"), nil
}

func unsupportedPartError(provider, model, role string, typ mdl.ContentPartType) error {
	return fmt.Errorf("%s adapter: model=%q role=%s unsupported content part type=%q", provider, model, role, typ)
}

func capabilityUnavailableError(provider, model, role string, typ mdl.ContentPartType, reason string) error {
	return fmt.Errorf("%s adapter: model=%q role=%s content part type=%q capability unavailable: %s", provider, model, role, typ, reason)
}

func fromToolCalls(tcs []openai.ChatCompletionMessageToolCall) []mdl.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	result := make([]mdl.ToolCall, len(tcs))
	for i, tc := range tcs {
		result[i] = mdl.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		}
	}
	return result
}

func fromUsage(u openai.CompletionUsage) mdl.TokenUsage {
	return mdl.TokenUsage{
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
	pending      []mdl.StreamChunk
	done         bool
	usage        mdl.TokenUsage
	toolBuilders map[int]*toolCallBuilder
}

func (it *streamIterator) Next() (mdl.StreamChunk, error) {
	for {
		if len(it.pending) > 0 {
			chunk := it.pending[0]
			it.pending = it.pending[1:]
			return chunk, nil
		}
		if it.done {
			return mdl.StreamChunk{}, io.EOF
		}
		if !it.stream.Next() {
			// 即使 SSE 末尾报错，也先尽量 flush 已累积的工具参数，减少截断影响。
			it.flushToolCalls()
			if err := it.stream.Err(); err != nil {
				if len(it.pending) > 0 {
					chunk := it.pending[0]
					it.pending = it.pending[1:]
					return chunk, nil
				}
				return mdl.StreamChunk{}, err
			}
			// 流正常结束，发出完成信号
			it.pending = append(it.pending, mdl.StreamChunk{
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
		it.usage = mdl.TokenUsage{
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
		it.pending = append(it.pending, mdl.StreamChunk{Delta: delta.Content})
	}
	if reasoning := extractReasoningText(delta.RawJSON()); reasoning != "" {
		it.pending = append(it.pending, mdl.StreamChunk{ReasoningDelta: reasoning})
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
		args = normalizeJSONArguments(args)
		tc := mdl.ToolCall{
			ID:        tb.id,
			Name:      tb.name,
			Arguments: json.RawMessage(args),
		}
		it.pending = append(it.pending, mdl.StreamChunk{ToolCall: &tc})
		delete(it.toolBuilders, idx)
	}
}

func (it *streamIterator) Close() error {
	it.done = true
	return it.stream.Close()
}

func normalizeJSONArguments(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "{}"
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}
	repaired := repairTruncatedJSON(trimmed)
	if json.Valid([]byte(repaired)) {
		return repaired
	}
	return trimmed
}

func repairTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	stack := make([]rune, 0, 8)
	inString := false
	escaped := false
	for _, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if inString && escaped {
		s += `\`
	}
	if inString {
		s += `"`
	}
	s = strings.TrimRight(s, ", \t\r\n")
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}

func extractReasoningText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := extractReasoningValue(payload[key]); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func extractReasoningValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractReasoningValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content"} {
			if text := extractReasoningValue(v[key]); text != "" {
				return text
			}
		}
	}
	return ""
}
