package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

var _ model.LLM = (*ResponsesClient)(nil)

// ResponsesClient 是 OpenAI Responses API 适配器。
type ResponsesClient struct {
	client    openai.Client
	model     string
	maxTokens int64
}

// NewResponses 创建 OpenAI Responses API 适配器。
func NewResponses(apiKey string, opts ...Option) *ResponsesClient {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	return NewResponsesWithRequestOptions(reqOpts, opts...)
}

// NewResponsesWithBaseURL 创建可自定义 base URL 的 Responses API 适配器。
func NewResponsesWithBaseURL(apiKey, baseURL string, opts ...Option) *ResponsesClient {
	var reqOpts []option.RequestOption
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(baseURL))
	}
	return NewResponsesWithRequestOptions(reqOpts, opts...)
}

// NewResponsesWithRequestOptions 创建允许传入 request options 的 Responses API 适配器。
func NewResponsesWithRequestOptions(reqOpts []option.RequestOption, opts ...Option) *ResponsesClient {
	c := &ResponsesClient{
		client:    openai.NewClient(reqOpts...),
		model:     DefaultModel,
		maxTokens: 4096,
	}
	temp := &Client{model: c.model, maxTokens: c.maxTokens}
	for _, opt := range opts {
		opt(temp)
	}
	c.model = temp.model
	c.maxTokens = temp.maxTokens
	return c
}

// GenerateContent 使用 Responses API 生成结果。
func (c *ResponsesClient) GenerateContent(ctx context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		params, effectiveModel, err := c.buildResponsesParams(req)
		if err != nil {
			yield(model.StreamChunk{}, err)
			return
		}
		stream := c.client.Responses.NewStreaming(ctx, params)
		it := &responsesStreamIterator{
			stream:           stream,
			actualModel:      strings.TrimSpace(effectiveModel),
			functionBuilders: map[string]*responsesFunctionCallBuilder{},
			emittedToolCalls: map[string]struct{}{},
		}
		defer it.Close()

		for {
			chunk, err := it.Next()
			if err == io.EOF {
				return
			}
			if !yield(chunk.Normalized(), err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

type responsesFunctionCallBuilder struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

type responsesStreamIterator struct {
	stream           *ssestream.Stream[responses.ResponseStreamEventUnion]
	pending          []model.StreamChunk
	done             bool
	actualModel      string
	functionBuilders map[string]*responsesFunctionCallBuilder
	emittedToolCalls map[string]struct{}
}

func (it *responsesStreamIterator) Next() (model.StreamChunk, error) {
	for {
		if len(it.pending) > 0 {
			chunk := it.pending[0]
			it.pending = it.pending[1:]
			return chunk.Normalized(), nil
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

func (it *responsesStreamIterator) processEvent(event responses.ResponseStreamEventUnion) {
	eventType := strings.TrimSpace(event.Type)
	switch {
	case eventType == "response.output_text.delta":
		if delta := responsesStreamDelta(event); delta != "" {
			it.pending = append(it.pending, model.TextDeltaChunk(delta))
		}
	case strings.HasPrefix(eventType, "response.reasoning") && strings.HasSuffix(eventType, ".delta"):
		if delta := responsesStreamDelta(event); delta != "" {
			it.pending = append(it.pending, model.ReasoningDeltaChunk(delta))
		}
	case eventType == "response.refusal.delta":
		if delta := responsesStreamDelta(event); delta != "" {
			it.pending = append(it.pending, model.RefusalDeltaChunk(delta))
		}
	case eventType == "response.function_call_arguments.delta":
		builder := it.ensureFunctionBuilder(responsesEventKey(event.ItemID, event.OutputIndex))
		builder.Arguments.WriteString(responsesStreamDelta(event))
	case eventType == "response.output_item.added" || eventType == "response.output_item.done":
		it.processOutputItemEvent(event, eventType == "response.output_item.done")
	case isResponsesHostedToolProgressEvent(eventType):
		it.enqueueHostedTool(responsesHostedToolFromEvent(event, eventType))
	case eventType == "response.completed":
		it.finish(&event.Response, false)
	case eventType == "response.incomplete":
		it.finish(&event.Response, true)
	}
}

func (it *responsesStreamIterator) ensureFunctionBuilder(key string) *responsesFunctionCallBuilder {
	if existing, ok := it.functionBuilders[key]; ok {
		return existing
	}
	created := &responsesFunctionCallBuilder{}
	it.functionBuilders[key] = created
	return created
}

func (it *responsesStreamIterator) processOutputItemEvent(event responses.ResponseStreamEventUnion, isDone bool) {
	item := event.Item
	itemType := strings.TrimSpace(item.Type)
	key := responsesEventKey(strings.TrimSpace(item.ID), event.OutputIndex)
	if itemType == "function_call" {
		builder := it.ensureFunctionBuilder(key)
		if strings.TrimSpace(item.CallID) != "" {
			builder.ID = strings.TrimSpace(item.CallID)
		}
		if strings.TrimSpace(item.Name) != "" {
			builder.Name = strings.TrimSpace(item.Name)
		}
		if strings.TrimSpace(item.Arguments) != "" {
			builder.Arguments.Reset()
			builder.Arguments.WriteString(item.Arguments)
		}
		if isDone {
			it.enqueueToolCall(builder.ID, builder.Name, builder.Arguments.String())
			delete(it.functionBuilders, key)
		}
		return
	}
	if isResponsesHostedToolType(itemType) {
		it.enqueueHostedTool(responsesHostedToolFromItem(item))
	}
}

func (it *responsesStreamIterator) enqueueToolCall(id, name, arguments string) {
	args := normalizeJSONArguments(arguments)
	call := model.ToolCall{
		ID:        strings.TrimSpace(id),
		Name:      strings.TrimSpace(name),
		Arguments: json.RawMessage(args),
	}
	key := call.ID + "\x00" + call.Name + "\x00" + string(call.Arguments)
	if _, ok := it.emittedToolCalls[key]; ok {
		return
	}
	it.emittedToolCalls[key] = struct{}{}
	it.pending = append(it.pending, model.ToolCallChunk(&call))
}

func (it *responsesStreamIterator) enqueueHostedTool(event *model.HostedToolEvent) {
	if event == nil {
		return
	}
	it.pending = append(it.pending, model.HostedToolChunk(event))
}

func (it *responsesStreamIterator) finish(resp *responses.Response, incomplete bool) {
	completion := fromResponsesResponse(resp)
	if completion.Metadata == nil && it.actualModel != "" {
		completion.Metadata = &model.LLMCallMetadata{ActualModel: it.actualModel}
	}
	for i := range completion.ToolCalls {
		call := completion.ToolCalls[i]
		it.enqueueToolCall(call.ID, call.Name, string(call.Arguments))
	}
	stopReason := strings.TrimSpace(completion.StopReason)
	if incomplete {
		stopReason = "incomplete"
	}
	it.pending = append(it.pending, model.DoneChunk(stopReason, &completion.Usage, completion.Metadata))
	it.done = true
}

func (it *responsesStreamIterator) Close() error {
	it.done = true
	if it.stream == nil {
		return nil
	}
	return it.stream.Close()
}

func responsesEventKey(itemID string, outputIndex int64) string {
	if strings.TrimSpace(itemID) != "" {
		return strings.TrimSpace(itemID)
	}
	return strconv.FormatInt(outputIndex, 10)
}

func isResponsesHostedToolType(itemType string) bool {
	itemType = strings.TrimSpace(itemType)
	return strings.HasSuffix(itemType, "_call") && itemType != "function_call"
}

func isResponsesHostedToolProgressEvent(eventType string) bool {
	if !strings.HasPrefix(eventType, "response.") {
		return false
	}
	for _, prefix := range []string{
		"response.file_search_call.",
		"response.web_search_call.",
		"response.code_interpreter_call.",
		"response.code_interpreter_call_code.",
		"response.image_generation_call.",
	} {
		if strings.HasPrefix(eventType, prefix) {
			return true
		}
	}
	return false
}

func responsesHostedToolFromItem(item responses.ResponseOutputItemUnion) *model.HostedToolEvent {
	if !isResponsesHostedToolType(item.Type) {
		return nil
	}
	payload, err := json.Marshal(item)
	if err != nil {
		payload = nil
	}
	return &model.HostedToolEvent{
		ID:     strings.TrimSpace(item.ID),
		Name:   strings.TrimSpace(item.Type),
		Status: strings.TrimSpace(item.Status),
		Output: payload,
	}
}

func responsesHostedToolFromEvent(event responses.ResponseStreamEventUnion, eventType string) *model.HostedToolEvent {
	name := strings.TrimPrefix(eventType, "response.")
	for _, suffix := range []string{".searching", ".in_progress", ".completed", ".interpreting", ".delta", ".done"} {
		name = strings.TrimSuffix(name, suffix)
	}
	status := eventType[strings.LastIndex(eventType, ".")+1:]
	payload, err := json.Marshal(event)
	if err != nil {
		payload = nil
	}
	return &model.HostedToolEvent{
		ID:     strings.TrimSpace(event.ItemID),
		Name:   strings.TrimSpace(name),
		Status: strings.TrimSpace(status),
		Output: payload,
	}
}

func responsesStreamDelta(event responses.ResponseStreamEventUnion) string {
	switch variant := event.AsAny().(type) {
	case responses.ResponseTextDeltaEvent:
		return variant.Delta
	case responses.ResponseReasoningSummaryTextDeltaEvent:
		return variant.Delta
	case responses.ResponseRefusalDeltaEvent:
		return variant.Delta
	case responses.ResponseFunctionCallArgumentsDeltaEvent:
		return variant.Delta
	default:
		return ""
	}
}

func (c *ResponsesClient) buildResponsesParams(req model.CompletionRequest) (responses.ResponseNewParams, string, error) {
	effectiveModel := c.model
	if strings.TrimSpace(req.Config.Model) != "" {
		effectiveModel = strings.TrimSpace(req.Config.Model)
	}
	instructions, inputItems, err := toResponsesInputItems(req.Messages, effectiveModel)
	if err != nil {
		return responses.ResponseNewParams{}, effectiveModel, err
	}
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(effectiveModel),
	}
	if strings.TrimSpace(instructions) != "" {
		params.Instructions = openai.String(instructions)
	}
	if len(inputItems) > 0 {
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems}
	}
	maxTokens := c.maxTokens
	if req.Config.MaxTokens > 0 {
		maxTokens = int64(req.Config.MaxTokens)
	}
	if maxTokens > 0 {
		params.MaxOutputTokens = openai.Int(maxTokens)
	}
	if req.Config.Temperature > 0 {
		params.Temperature = openai.Float(req.Config.Temperature)
	}
	tools, err := toResponsesTools(req.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, effectiveModel, err
	}
	if len(tools) > 0 {
		params.Tools = tools
		params.ParallelToolCalls = openai.Bool(true)
	}
	if req.ResponseFormat != nil {
		textCfg, err := toResponsesTextConfig(req.ResponseFormat)
		if err != nil {
			return responses.ResponseNewParams{}, effectiveModel, err
		}
		params.Text = textCfg
	}
	return params, effectiveModel, nil
}

func toResponsesInputItems(msgs []model.Message, modelName string) (string, responses.ResponseInputParam, error) {
	msgs = normalizeMessages(msgs)
	var instructions []string
	items := make(responses.ResponseInputParam, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case model.RoleSystem:
			content, err := contentPartsToTextOnlyString(msg.ContentParts, "openai-responses", modelName, "system")
			if err != nil {
				return "", nil, err
			}
			if strings.TrimSpace(content) != "" {
				instructions = append(instructions, content)
			}
		case model.RoleUser, model.RoleAssistant:
			content, err := contentPartsToTextOnlyString(msg.ContentParts, "openai-responses", modelName, string(msg.Role))
			if err != nil {
				return "", nil, err
			}
			if strings.TrimSpace(content) != "" {
				items = append(items, responses.ResponseInputItemUnionParam{
					OfMessage: &responses.EasyInputMessageParam{
						Role: responses.EasyInputMessageRole(msg.Role),
						Content: responses.EasyInputMessageContentUnionParam{
							OfString: openai.String(content),
						},
					},
				})
			}
			for index, tc := range msg.ToolCalls {
				callID := strings.TrimSpace(tc.ID)
				if callID == "" {
					callID = fmt.Sprintf("%s-%d", strings.TrimSpace(tc.Name), index+1)
				}
				items = append(items, responses.ResponseInputItemUnionParam{
					OfFunctionCall: &responses.ResponseFunctionToolCallParam{
						CallID:    callID,
						Name:      strings.TrimSpace(tc.Name),
						Arguments: normalizeJSONArguments(string(tc.Arguments)),
					},
				})
			}
		case model.RoleTool:
			for _, tr := range msg.ToolResults {
				content, err := contentPartsToTextOnlyString(tr.ContentParts, "openai-responses", modelName, "tool_result")
				if err != nil {
					return "", nil, err
				}
				items = append(items, responses.ResponseInputItemUnionParam{
					OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
						CallID: strings.TrimSpace(tr.CallID),
						Output: content,
					},
				})
			}
		}
	}
	return strings.Join(instructions, "\n\n"), items, nil
}

func toResponsesTools(tools []model.ToolSpec) ([]responses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]responses.ToolUnionParam, len(tools))
	for i, spec := range tools {
		params := map[string]any{}
		if len(spec.InputSchema) > 0 {
			if err := json.Unmarshal(spec.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("unmarshal tool schema for %s: %w", spec.Name, err)
			}
		}
		function := &responses.FunctionToolParam{
			Name:       spec.Name,
			Parameters: params,
			Strict:     openai.Bool(true),
		}
		if strings.TrimSpace(spec.Description) != "" {
			function.Description = openai.String(spec.Description)
		}
		result[i] = responses.ToolUnionParam{OfFunction: function}
	}
	return result, nil
}

func toResponsesTextConfig(format *model.ResponseFormat) (responses.ResponseTextConfigParam, error) {
	if format == nil {
		return responses.ResponseTextConfigParam{}, nil
	}
	switch format.Type {
	case "", "text":
		return responses.ResponseTextConfigParam{}, nil
	case "json_object":
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
			},
		}, nil
	case "json_schema":
		if format.JSONSchema == nil {
			return responses.ResponseTextConfigParam{}, fmt.Errorf("json_schema response format requires schema details")
		}
		var schema map[string]any
		if len(format.JSONSchema.Schema) > 0 {
			if err := json.Unmarshal(format.JSONSchema.Schema, &schema); err != nil {
				return responses.ResponseTextConfigParam{}, fmt.Errorf("unmarshal json_schema: %w", err)
			}
		}
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   format.JSONSchema.Name,
					Schema: schema,
					Strict: openai.Bool(format.JSONSchema.Strict),
				},
			},
		}, nil
	default:
		return responses.ResponseTextConfigParam{}, fmt.Errorf("unsupported response format type %q", format.Type)
	}
}

func fromResponsesResponse(resp *responses.Response) *model.CompletionResponse {
	completion := &model.CompletionResponse{
		Message: model.Message{Role: model.RoleAssistant},
	}
	if resp == nil {
		completion.StopReason = "end_turn"
		return completion
	}
	completion.Usage = extractResponsesUsage(resp)
	for _, item := range resp.Output {
		switch strings.TrimSpace(item.Type) {
		case "reasoning":
			if reasoning := extractResponsesReasoning(item); reasoning != "" {
				completion.Message.ContentParts = append(completion.Message.ContentParts, model.ReasoningPart(reasoning))
			}
		case "message":
			if text := extractResponsesOutputText(item.Content); text != "" {
				completion.Message.ContentParts = append(completion.Message.ContentParts, model.TextPart(text))
			} else if refusal := extractResponsesOutputRefusal(item.Content); refusal != "" {
				completion.Message.ContentParts = append(completion.Message.ContentParts, model.RefusalPart(refusal))
			}
		case "function_call":
			arguments := normalizeJSONArguments(item.Arguments)
			completion.ToolCalls = append(completion.ToolCalls, model.ToolCall{
				ID:        strings.TrimSpace(item.CallID),
				Name:      strings.TrimSpace(item.Name),
				Arguments: json.RawMessage(arguments),
			})
		default:
			if hosted := responsesHostedToolFromItem(item); hosted != nil {
				completion.Message.HostedToolCalls = append(completion.Message.HostedToolCalls, *hosted)
			}
		}
	}
	if len(completion.ToolCalls) > 0 {
		completion.StopReason = "tool_use"
		completion.Message.ToolCalls = append([]model.ToolCall(nil), completion.ToolCalls...)
	} else if model.ContentPartsToRefusalText(completion.Message.ContentParts) != "" && model.ContentPartsToTextOnly(completion.Message.ContentParts) == "" {
		completion.StopReason = "refusal"
	} else {
		completion.StopReason = "end_turn"
	}
	return completion
}

func extractResponsesOutputText(contents []responses.ResponseOutputMessageContentUnion) string {
	parts := make([]string, 0, len(contents))
	for _, part := range contents {
		if strings.TrimSpace(part.Type) == "output_text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func extractResponsesOutputRefusal(contents []responses.ResponseOutputMessageContentUnion) string {
	parts := make([]string, 0, len(contents))
	for _, part := range contents {
		if strings.TrimSpace(part.Type) == "refusal" && strings.TrimSpace(part.Refusal) != "" {
			parts = append(parts, strings.TrimSpace(part.Refusal))
		}
	}
	return strings.Join(parts, "\n")
}

func extractResponsesReasoning(item responses.ResponseOutputItemUnion) string {
	raw, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(fmt.Sprint(payload["type"])) != "reasoning" {
		return ""
	}
	if text := extractReasoningValue(payload["summary"]); text != "" {
		return text
	}
	return extractReasoningValue(payload["content"])
}

func extractResponsesUsage(resp *responses.Response) model.TokenUsage {
	raw, err := json.Marshal(resp)
	if err != nil {
		return model.TokenUsage{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return model.TokenUsage{}
	}
	usage, _ := payload["usage"].(map[string]any)
	promptTokens := extractNumericValue(usage["input_tokens"])
	if promptTokens == 0 {
		promptTokens = extractNumericValue(usage["prompt_tokens"])
	}
	completionTokens := extractNumericValue(usage["output_tokens"])
	if completionTokens == 0 {
		completionTokens = extractNumericValue(usage["completion_tokens"])
	}
	totalTokens := extractNumericValue(usage["total_tokens"])
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	return model.TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

func extractNumericValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}
