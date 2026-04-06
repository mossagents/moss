package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/logging"
)

// LoopConfig 配置 Agent Loop 的行为。
type LoopConfig struct {
	MaxIterations      int                     // 最大循环次数（默认 50）
	StopWhen           func(port.Message) bool // 自定义停止条件
	ParallelToolCall   bool                    // 启用并行工具调用（默认 false，串行执行）
	MaxConcurrentTools int                     // 并行工具调用的最大并发数（默认 8，0 表示使用默认值）
	LLMRetry           RetryConfig             // LLM 调用重试配置
	LLMBreaker         *retry.Breaker          // LLM 调用熔断器（可选）
}

// RetryConfig 复用 retry.Config，避免 loop 与其他组件维护多套重试配置定义。
type RetryConfig = retry.Config

type callAttemptResult struct {
	resp      *port.CompletionResponse
	streamed  bool
	retryable bool
	err       error
}

func (c LoopConfig) maxIter() int {
	if c.MaxIterations <= 0 {
		return 50
	}
	return c.MaxIterations
}

// AgentLoop 组合所有子系统，驱动 Agent 的 think→act→observe 循环。
type AgentLoop struct {
	LLM               port.LLM
	Tools             tool.Registry
	Chain             *middleware.Chain
	IO                port.UserIO
	Config            LoopConfig
	Observer          port.Observer // 可观测性观察者（可选，默认 NoOpObserver）
	LifecycleHook     session.LifecycleHook
	ToolLifecycleHook session.ToolLifecycleHook
	RunID             string
	sidefxMu          sync.Mutex
	eventSeq          uint64
	currentTurn       TurnPlan
}

// SessionResult 是一次 Session 执行的结果。
type SessionResult struct {
	SessionID  string          `json:"session_id"`
	Success    bool            `json:"success"`
	Output     string          `json:"output"`
	Steps      int             `json:"steps"`
	TokensUsed port.TokenUsage `json:"tokens_used"`
	Error      string          `json:"error,omitempty"`
}

func (l *AgentLoop) observer() port.Observer {
	if l.Observer != nil {
		return l.Observer
	}
	return port.NoOpObserver{}
}

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session, plan TurnPlan) (*port.CompletionResponse, bool, error) {
	specs := l.toolSpecs(plan)
	promptMessages := session.PromptMessages(sess)
	logging.GetLogger().DebugContext(ctx, "llm request prepared",
		slog.String("session_id", sess.ID),
		slog.String("turn_id", plan.TurnID),
		slog.String("model_lane", plan.ModelRoute.Lane),
		slog.Int("messages", len(promptMessages)),
		slog.Int("tools", len(specs)),
		slog.Int("estimated_tokens", session.EstimateMessagesTokens(promptMessages)),
	)
	modelConfig := sess.Config.ModelConfig
	modelConfig.Requirements = cloneTaskRequirement(plan.ModelRoute.Requirements)
	req := port.CompletionRequest{
		Messages: promptMessages,
		Tools:    specs,
		Config:   modelConfig,
	}

	cfg := l.Config.LLMRetry
	if !cfg.Enabled() {
		attempt := l.callLLMOnce(ctx, req)
		return attempt.resp, attempt.streamed, attempt.err
	}

	maxRetries := cfg.MaxRetriesOrDefault()
	delay := cfg.InitialDelayOrDefault()
	var lastErr error

	for attemptIndex := 0; attemptIndex <= maxRetries; attemptIndex++ {
		attempt := l.callLLMOnce(ctx, req)
		if attempt.err == nil {
			return attempt.resp, attempt.streamed, nil
		}

		lastErr = attempt.err
		if !attempt.retryable || !cfg.ShouldRetryOrDefault(ctx, attempt.err) || attemptIndex == maxRetries {
			return nil, false, attempt.err
		}

		jitter := time.Duration(rand.Int63n(int64(delay) / 2))
		sleepDuration := delay + jitter
		if sleepDuration > cfg.MaxDelayOrDefault() {
			sleepDuration = cfg.MaxDelayOrDefault()
		}

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(sleepDuration):
		}

		delay = time.Duration(float64(delay) * cfg.MultiplierOrDefault())
		if delay > cfg.MaxDelayOrDefault() {
			delay = cfg.MaxDelayOrDefault()
		}
	}

	return nil, false, lastErr
}

func (l *AgentLoop) callLLMOnce(ctx context.Context, req port.CompletionRequest) callAttemptResult {
	// 熔断器检查
	if b := l.Config.LLMBreaker; b != nil {
		if !b.Allow() {
			return callAttemptResult{
				err: &port.LLMCallError{
					Err:       kerrors.New(kerrors.ErrLLMRejected, "LLM circuit breaker is open: too many recent failures"),
					Retryable: false,
				},
				retryable: false,
			}
		}
	}

	// 优先使用 Streaming
	if sllm, ok := l.LLM.(port.StreamingLLM); ok {
		resp, err := l.streamLLM(ctx, sllm, req)
		if err == nil {
			l.recordBreakerSuccess()
			return callAttemptResult{resp: resp, streamed: true}
		}
		if llmErrorFallbackSafe(err) {
			if fallbackResp, fallbackErr := l.LLM.Complete(ctx, req); fallbackErr == nil {
				l.recordBreakerSuccess()
				return callAttemptResult{resp: fallbackResp, streamed: false}
			} else {
				err = fallbackErr
			}
		}

		l.recordBreakerFailure()
		return callAttemptResult{streamed: true, retryable: llmErrorRetryable(err), err: err}
	}

	resp, err := l.LLM.Complete(ctx, req)
	if err != nil {
		l.recordBreakerFailure()
	} else {
		l.recordBreakerSuccess()
	}
	return callAttemptResult{resp: resp, streamed: false, retryable: llmErrorRetryable(err), err: err}
}

func (l *AgentLoop) streamLLM(ctx context.Context, sllm port.StreamingLLM, req port.CompletionRequest) (*port.CompletionResponse, error) {
	iter, err := sllm.Stream(ctx, req)
	if err != nil {
		return nil, ensureLLMCallError(err, true, true, port.LLMCallMetadata{})
	}
	defer iter.Close()
	metadataProvider := metadataStreamProvider(iter)
	state := streamAccumulator{}

	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			shouldContinue, handledErr := l.handleStreamChunkError(ctx, err, metadataProvider, &state)
			if shouldContinue {
				break
			}
			return nil, handledErr
		}
		if done := l.applyStreamChunk(ctx, chunk, &state); done {
			break
		}
	}

	msg := port.Message{
		Role:      port.RoleAssistant,
		ToolCalls: state.toolCalls,
	}
	if state.fullReasoning != "" {
		msg.ContentParts = append(msg.ContentParts, port.ReasoningPart(state.fullReasoning))
	}
	if state.fullContent != "" {
		msg.ContentParts = append(msg.ContentParts, port.TextPart(state.fullContent))
	}

	return &port.CompletionResponse{
		Message:    msg,
		ToolCalls:  state.toolCalls,
		Usage:      state.usage,
		StopReason: state.stopReason,
		Metadata:   metadataPtr(streamMetadata(metadataProvider)),
	}, nil
}

type streamAccumulator struct {
	fullContent    string
	fullReasoning  string
	toolCalls      []port.ToolCall
	usage          port.TokenUsage
	stopReason     string
	emittedContent bool
}

func metadataStreamProvider(iter port.StreamIterator) port.MetadataStreamIterator {
	if provider, ok := iter.(port.MetadataStreamIterator); ok {
		return provider
	}
	return nil
}

func (l *AgentLoop) handleStreamChunkError(
	ctx context.Context,
	err error,
	metadataProvider port.MetadataStreamIterator,
	state *streamAccumulator,
) (bool, error) {
	if state.emittedContent && len(state.toolCalls) > 0 && isRecoverableStreamTailError(err) {
		state.stopReason = "tool_use"
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{Type: port.OutputStreamEnd})
		}
		return true, nil
	}
	safePreEmission := !state.emittedContent && len(state.toolCalls) == 0
	return false, ensureLLMCallError(err, safePreEmission, safePreEmission, streamMetadata(metadataProvider))
}

func (l *AgentLoop) applyStreamChunk(ctx context.Context, chunk port.StreamChunk, state *streamAccumulator) bool {
	if chunk.ReasoningDelta != "" {
		state.emittedContent = true
		state.fullReasoning += chunk.ReasoningDelta
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputReasoning,
				Content: chunk.ReasoningDelta,
			})
		}
	}

	if chunk.Delta != "" {
		state.emittedContent = true
		state.fullContent += chunk.Delta
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputStream,
				Content: chunk.Delta,
			})
		}
	}

	if chunk.ToolCall != nil {
		state.emittedContent = true
		state.toolCalls = append(state.toolCalls, *chunk.ToolCall)
	}

	if !chunk.Done {
		return false
	}
	if chunk.Usage != nil {
		state.usage = *chunk.Usage
	}
	state.stopReason = "end_turn"
	if len(state.toolCalls) > 0 {
		state.stopReason = "tool_use"
	}
	if l.IO != nil {
		l.IO.Send(ctx, port.OutputMessage{Type: port.OutputStreamEnd})
	}
	return true
}

func isRecoverableStreamTailError(err error) bool {
	if err == nil {
		return false
	}
	// io.ErrUnexpectedEOF is the canonical sentinel for a truncated stream body.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// json.SyntaxError "unexpected end of JSON input" indicates the stream was
	// cut off mid-object; use errors.As so wrapped errors are handled correctly.
	var jsonErr *json.SyntaxError
	if errors.As(err, &jsonErr) {
		return strings.Contains(strings.ToLower(jsonErr.Error()), "unexpected end of json input")
	}
	return false
}

func streamMetadata(provider port.MetadataStreamIterator) port.LLMCallMetadata {
	if provider == nil {
		return port.LLMCallMetadata{}
	}
	return provider.Metadata()
}

func metadataPtr(meta port.LLMCallMetadata) *port.LLMCallMetadata {
	if strings.TrimSpace(meta.ActualModel) == "" && len(meta.Attempts) == 0 {
		return nil
	}
	copyMeta := meta
	return &copyMeta
}

func ensureLLMCallError(err error, retryable, fallbackSafe bool, metadata port.LLMCallMetadata) error {
	if err == nil {
		return nil
	}
	var callErr *port.LLMCallError
	if errors.As(err, &callErr) {
		merged := *callErr
		merged.Metadata = mergeLLMMetadata(merged.Metadata, metadata)
		return &merged
	}
	return &port.LLMCallError{
		Err:          err,
		Retryable:    retryable,
		FallbackSafe: fallbackSafe,
		Metadata:     metadata,
	}
}

func mergeLLMMetadata(base, overlay port.LLMCallMetadata) port.LLMCallMetadata {
	if strings.TrimSpace(base.ActualModel) == "" {
		base.ActualModel = overlay.ActualModel
	}
	if len(overlay.Attempts) > 0 {
		base.Attempts = append(base.Attempts, overlay.Attempts...)
	}
	return base
}

func llmErrorRetryable(err error) bool {
	if err == nil {
		return false
	}
	var callErr *port.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.Retryable
	}
	return true
}

func llmErrorFallbackSafe(err error) bool {
	var callErr *port.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.FallbackSafe
	}
	return false
}

func llmMetadataFromResponse(defaultModel string, resp *port.CompletionResponse) port.LLMCallMetadata {
	if resp == nil || resp.Metadata == nil {
		return port.LLMCallMetadata{ActualModel: defaultModel}
	}
	meta := *resp.Metadata
	if strings.TrimSpace(meta.ActualModel) == "" {
		meta.ActualModel = defaultModel
	}
	return meta
}

func llmMetadataFromError(defaultModel string, err error) port.LLMCallMetadata {
	var callErr *port.LLMCallError
	if errors.As(err, &callErr) {
		meta := callErr.Metadata
		if strings.TrimSpace(meta.ActualModel) == "" {
			meta.ActualModel = defaultModel
		}
		return meta
	}
	return port.LLMCallMetadata{ActualModel: defaultModel}
}

func (l *AgentLoop) emitLLMAttemptEvents(ctx context.Context, sessionID string, metadata port.LLMCallMetadata, exhausted bool) {
	for _, attempt := range metadata.Attempts {
		event := l.executionEventBase(&session.Session{ID: sessionID}, port.ExecutionEventType("llm_failover_attempt"), "llm", "runtime", "llm_attempt")
		event.Model = attempt.CandidateModel
		event.Data = map[string]any{
			"candidate_model": attempt.CandidateModel,
			"attempt_index":   attempt.AttemptIndex,
			"candidate_retry": attempt.CandidateRetry,
			"failure_reason":  attempt.FailureReason,
			"breaker_state":   attempt.BreakerState,
			"failover_to":     attempt.FailoverTo,
			"outcome":         attempt.Outcome,
			"model_lane":      l.currentTurn.ModelRoute.Lane,
		}
		port.ObserveExecutionEvent(ctx, l.observer(), event)
		if strings.TrimSpace(attempt.FailoverTo) != "" {
			switchEvent := l.executionEventBase(&session.Session{ID: sessionID}, port.ExecutionEventType("llm_failover_switch"), "llm", "runtime", "llm_attempt")
			switchEvent.Model = attempt.CandidateModel
			switchEvent.Data = map[string]any{
				"candidate_model": attempt.CandidateModel,
				"failover_to":     attempt.FailoverTo,
				"model_lane":      l.currentTurn.ModelRoute.Lane,
			}
			port.ObserveExecutionEvent(ctx, l.observer(), switchEvent)
		}
	}
	if exhausted && len(metadata.Attempts) > 0 {
		event := l.executionEventBase(&session.Session{ID: sessionID}, port.ExecutionEventType("llm_failover_exhausted"), "llm", "runtime", "llm_attempt")
		event.Model = metadata.ActualModel
		port.ObserveExecutionEvent(ctx, l.observer(), event)
	}
}

func (l *AgentLoop) executeToolCalls(ctx context.Context, sess *session.Session, calls []port.ToolCall) error {
	if l.Config.ParallelToolCall && len(calls) > 1 {
		return l.executeToolCallsParallel(ctx, sess, calls)
	}
	return l.executeToolCallsSerial(ctx, sess, calls)
}

func (l *AgentLoop) executeToolCallsSerial(ctx context.Context, sess *session.Session, calls []port.ToolCall) error {
	for _, call := range calls {
		result := l.executeSingleToolCall(ctx, sess, call)
		sess.AppendMessage(port.Message{Role: port.RoleTool, ToolResults: []port.ToolResult{result}})
	}
	return nil
}

func (l *AgentLoop) maxConcurrentTools() int {
	if l.Config.MaxConcurrentTools > 0 {
		return l.Config.MaxConcurrentTools
	}
	return 8
}

func (l *AgentLoop) executeToolCallsParallel(ctx context.Context, sess *session.Session, calls []port.ToolCall) error {
	results := make([]port.ToolResult, len(calls))

	sem := make(chan struct{}, l.maxConcurrentTools())
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c port.ToolCall) {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()
			results[idx] = l.executeSingleToolCall(ctx, sess, c)
		}(i, call)
	}
	wg.Wait()

	// 按顺序追加结果到 session（保持确定性）
	for _, result := range results {
		sess.AppendMessage(port.Message{Role: port.RoleTool, ToolResults: []port.ToolResult{result}})
	}
	return nil
}

func (l *AgentLoop) executeSingleToolCall(ctx context.Context, sess *session.Session, call port.ToolCall) port.ToolResult {
	repairedArgs := repairToolArguments(call.Arguments)
	l.emitToolLifecycle(ctx, session.ToolLifecycleEvent{
		Stage:     session.ToolLifecycleBefore,
		Session:   sess,
		ToolName:  call.Name,
		CallID:    call.ID,
		Arguments: repairedArgs,
		Timestamp: time.Now().UTC(),
	})
	if !l.toolAllowed(call.Name) {
		return l.handleMissingTool(ctx, sess, call, repairedArgs)
	}
	spec, handler, ok := l.Tools.Get(call.Name)
	if !ok {
		return l.handleMissingTool(ctx, sess, call, repairedArgs)
	}

	// Validate required fields declared in the tool's input schema.
	// This is a best-effort guard against malformed or prompt-injected args.
	if err := validateRequiredToolArgs(spec, repairedArgs); err != nil {
		schemaErr := fmt.Errorf("tool %q argument validation failed: %w", call.Name, err)
		return buildToolResult(call.ID, nil, schemaErr)
	}

	l.emitToolStarted(ctx, sess, call, spec, repairedArgs)

	beforeErr := l.runBeforeToolCallMiddleware(ctx, sess, spec, call.Arguments)
	if beforeErr != nil {
		return l.handleBeforeToolCallError(ctx, sess, call, spec, repairedArgs, beforeErr)
	}

	toolCtx := port.WithToolCallContext(ctx, port.ToolCallContext{
		SessionID: sess.ID,
		ToolName:  call.Name,
		CallID:    call.ID,
	})
	// 执行工具
	toolStart := time.Now()
	output, err := handler(toolCtx, repairedArgs)
	toolDur := time.Since(toolStart)
	result := buildToolResult(call.ID, output, err)
	l.observeToolCompletion(ctx, sess, call, spec, toolStart, toolDur, result, output, err)
	l.runAfterToolCallMiddleware(ctx, sess, spec, output)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, spec, result, toolDur, err)
	l.sendToolResultIO(ctx, call, result, toolDur, err)
	return result
}

func buildToolResult(callID string, output []byte, err error) port.ToolResult {
	if err != nil {
		return port.ToolResult{
			CallID:       callID,
			ContentParts: []port.ContentPart{port.TextPart(err.Error())},
			IsError:      true,
		}
	}
	return port.ToolResult{
		CallID:       callID,
		ContentParts: []port.ContentPart{port.TextPart(string(output))},
	}
}

// validateRequiredToolArgs checks that all fields listed as "required" in the
// tool's input schema are present in the provided (repaired) arguments.
// It is a best-effort guard: if the schema cannot be parsed the call proceeds.
func validateRequiredToolArgs(spec tool.ToolSpec, args json.RawMessage) error {
	if len(spec.InputSchema) == 0 || len(args) == 0 {
		return nil
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil || len(schema.Required) == 0 {
		return nil // best-effort: unparseable schema or no required fields
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	for _, field := range schema.Required {
		if _, ok := obj[field]; !ok {
			return fmt.Errorf("missing required argument %q", field)
		}
	}
	return nil
}



func (l *AgentLoop) handleMissingTool(ctx context.Context, sess *session.Session, call port.ToolCall, repairedArgs json.RawMessage) port.ToolResult {
	err := fmt.Errorf("tool %q not found or not allowed in current turn", call.Name)
	result := buildToolResult(call.ID, nil, err)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, tool.ToolSpec{}, result, 0, err)
	return result
}

func (l *AgentLoop) emitToolStarted(ctx context.Context, sess *session.Session, call port.ToolCall, spec tool.ToolSpec, repairedArgs json.RawMessage) {
	if l.IO != nil {
		l.IO.Send(ctx, port.OutputMessage{
			Type:    port.OutputToolStart,
			Content: call.Name,
			Meta: map[string]any{
				"call_id":      call.ID,
				"tool":         call.Name,
				"risk":         string(spec.Risk),
				"args_preview": previewToolArguments(repairedArgs),
			},
		})
	}
	port.ObserveExecutionEvent(ctx, l.observer(), port.ExecutionEvent{
		Type:         port.ExecutionToolStarted,
		EventID:      l.nextEventID(string(port.ExecutionToolStarted)),
		EventVersion: 1,
		RunID:        strings.TrimSpace(l.RunID),
		TurnID:       strings.TrimSpace(l.currentTurn.TurnID),
		SessionID:    sess.ID,
		Timestamp:    time.Now().UTC(),
		Phase:        "tool",
		Actor:        "runtime",
		PayloadKind:  "tool",
		ToolName:     call.Name,
		CallID:       call.ID,
		Risk:         string(spec.Risk),
	})
}

func (l *AgentLoop) runBeforeToolCallMiddleware(ctx context.Context, sess *session.Session, spec tool.ToolSpec, input []byte) error {
	var err error
	l.withSideEffectsLock(func() {
		err = l.runMiddleware(ctx, middleware.BeforeToolCall, sess, &spec, input, nil)
	})
	return err
}

func (l *AgentLoop) runAfterToolCallMiddleware(ctx context.Context, sess *session.Session, spec tool.ToolSpec, output []byte) {
	l.withSideEffectsLock(func() {
		l.runMiddleware(ctx, middleware.AfterToolCall, sess, &spec, nil, output)
	})
}

func (l *AgentLoop) handleBeforeToolCallError(
	ctx context.Context,
	sess *session.Session,
	call port.ToolCall,
	spec tool.ToolSpec,
	repairedArgs json.RawMessage,
	beforeErr error,
) port.ToolResult {
	normalizedErr := normalizeToolError(beforeErr)
	result := buildToolResult(call.ID, nil, beforeErr)
	port.ObserveToolCall(ctx, l.observer(), port.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
		StartedAt: time.Now().UTC(),
		Duration:  0,
		Error:     normalizedErr,
	})
	event := l.executionEventBase(sess, port.ExecutionToolCompleted, "tool", "runtime", "tool")
	event.ToolName = call.Name
	event.CallID = call.ID
	event.Risk = string(spec.Risk)
	event.Data = map[string]any{
		"is_error": true,
	}
	event.Error = normalizedErr.Error()
	appendToolErrorMetadata(&event, normalizedErr)
	port.ObserveExecutionEvent(ctx, l.observer(), event)
	l.sendToolResultIO(ctx, call, result, 0, normalizedErr)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, spec, result, 0, normalizedErr)
	return result
}

func (l *AgentLoop) observeToolCompletion(
	ctx context.Context,
	sess *session.Session,
	call port.ToolCall,
	spec tool.ToolSpec,
	toolStart time.Time,
	toolDur time.Duration,
	result port.ToolResult,
	output []byte,
	err error,
) {
	port.ObserveToolCall(ctx, l.observer(), port.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
		StartedAt: toolStart.UTC(),
		Duration:  toolDur,
		Error:     err,
	})
	event := l.executionEventBase(sess, port.ExecutionToolCompleted, "tool", "runtime", "tool")
	event.ToolName = call.Name
	event.CallID = call.ID
	event.Risk = string(spec.Risk)
	event.Duration = toolDur
	event.Data = map[string]any{
		"is_error": result.IsError,
	}
	if err != nil {
		event.Error = err.Error()
		appendToolErrorMetadata(&event, err)
	}
	appendToolExecutionMetadata(&event, output)
	port.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) emitToolLifecycleAfter(
	ctx context.Context,
	sess *session.Session,
	call port.ToolCall,
	repairedArgs json.RawMessage,
	spec tool.ToolSpec,
	result port.ToolResult,
	toolDur time.Duration,
	err error,
) {
	l.emitToolLifecycle(ctx, session.ToolLifecycleEvent{
		Stage:     session.ToolLifecycleAfter,
		Session:   sess,
		ToolName:  call.Name,
		CallID:    call.ID,
		Arguments: repairedArgs,
		Result:    &result,
		Risk:      string(spec.Risk),
		Duration:  toolDur,
		Error:     err,
		Timestamp: time.Now().UTC(),
	})
}

func (l *AgentLoop) sendToolResultIO(ctx context.Context, call port.ToolCall, result port.ToolResult, toolDur time.Duration, err error) {
	if l.IO == nil {
		return
	}
	meta := map[string]any{
		"call_id":     call.ID,
		"tool":        call.Name,
		"is_error":    result.IsError,
		"duration_ms": toolDur.Milliseconds(),
	}
	appendToolErrorIOMetadata(meta, err)
	l.IO.Send(ctx, port.OutputMessage{
		Type:    port.OutputToolResult,
		Content: port.ContentPartsToPlainText(result.ContentParts),
		Meta:    meta,
	})
}

func (l *AgentLoop) emitToolLifecycle(ctx context.Context, event session.ToolLifecycleEvent) {
	if l.ToolLifecycleHook == nil {
		return
	}
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	defer func() {
		if r := recover(); r != nil {
			sessionID := ""
			if event.Session != nil {
				sessionID = event.Session.ID
			}
			err := fmt.Errorf("tool lifecycle hook panic: %v", r)
			slog.Default().ErrorContext(callCtx, "tool lifecycle hook panic",
				slog.String("stage", string(event.Stage)),
				slog.String("session_id", sessionID),
				slog.String("tool", event.ToolName),
				slog.String("call_id", event.CallID),
				slog.Any("panic", r),
			)
			port.ObserveError(context.Background(), l.observer(), port.ErrorEvent{
				SessionID: sessionID,
				Phase:     "tool_lifecycle_hook",
				Error:     err,
				Message:   err.Error(),
			})
		}
	}()
	l.ToolLifecycleHook(callCtx, event)
}

func appendToolExecutionMetadata(event *port.ExecutionEvent, output json.RawMessage) {
	if event == nil || len(output) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		return
	}
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	for _, key := range []string{"enforcement", "degraded", "details", "url", "method", "status_code", "follow_redirects"} {
		if value, ok := payload[key]; ok {
			event.Data[key] = value
		}
	}
}

func appendExecutionErrorMetadata(event *port.ExecutionEvent, err error) {
	if event == nil || err == nil {
		return
	}
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	code := string(kerrors.GetCode(err))
	if code != "" {
		event.Data["error_code"] = code
	}
	var kernelErr *kerrors.Error
	if errors.As(err, &kernelErr) && len(kernelErr.Meta) > 0 {
		for k, v := range kernelErr.Meta {
			event.Data[k] = v
		}
	}
}

func appendToolErrorMetadata(event *port.ExecutionEvent, err error) {
	appendExecutionErrorMetadata(event, err)
}

func appendToolErrorIOMetadata(meta map[string]any, err error) {
	if meta == nil || err == nil {
		return
	}
	code := string(kerrors.GetCode(err))
	if code != "" {
		meta["error_code"] = code
	}
	var kernelErr *kerrors.Error
	if errors.As(err, &kernelErr) && len(kernelErr.Meta) > 0 {
		for _, key := range []string{"reason_code", "reason", "enforcement", "tool"} {
			if value, ok := kernelErr.Meta[key]; ok {
				meta[key] = value
			}
		}
	}
}

type kernelErrorProvider interface {
	AsKernelError() *kerrors.Error
}

func normalizeToolError(err error) error {
	if err == nil {
		return nil
	}
	var provider kernelErrorProvider
	if errors.As(err, &provider) {
		if wrapped := provider.AsKernelError(); wrapped != nil {
			return wrapped
		}
	}
	return err
}

func (l *AgentLoop) withSideEffectsLock(fn func()) {
	l.sidefxMu.Lock()
	defer l.sidefxMu.Unlock()
	fn()
}

func (l *AgentLoop) toolSpecs(plan TurnPlan) []port.ToolSpec {
	allowed := allowedToolNames(plan.ToolRoute)
	if len(allowed) == 0 {
		return nil
	}
	tools := tool.Scoped(l.Tools, allowed).List()
	specs := make([]port.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = port.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return specs
}

func (l *AgentLoop) toolAllowed(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if len(l.currentTurn.ToolRoute) == 0 {
		return true
	}
	for _, decision := range l.currentTurn.ToolRoute {
		if decision.Name != name {
			continue
		}
		return decision.Status != ToolRouteHidden
	}
	return false
}

func (l *AgentLoop) nextEventID(prefix string) string {
	seq := atomic.AddUint64(&l.eventSeq, 1)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "evt"
	}
	runID := strings.TrimSpace(l.RunID)
	if runID == "" {
		runID = "run"
	}
	return runID + "-" + prefix + "-" + strconv.FormatUint(seq, 10)
}

func (l *AgentLoop) executionEventBase(sess *session.Session, eventType port.ExecutionEventType, phase, actor, payloadKind string) port.ExecutionEvent {
	return port.ExecutionEvent{
		Type:         eventType,
		EventID:      l.nextEventID(string(eventType)),
		EventVersion: 1,
		RunID:        strings.TrimSpace(l.RunID),
		TurnID:       strings.TrimSpace(l.currentTurn.TurnID),
		SessionID:    sessionIDOf(sess),
		Timestamp:    time.Now().UTC(),
		Phase:        strings.TrimSpace(phase),
		Actor:        strings.TrimSpace(actor),
		PayloadKind:  strings.TrimSpace(payloadKind),
	}
}

func sessionIDOf(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.ID)
}

func (l *AgentLoop) recordBreakerSuccess() {
	if b := l.Config.LLMBreaker; b != nil {
		b.RecordSuccess()
	}
}

func (l *AgentLoop) recordBreakerFailure() {
	if b := l.Config.LLMBreaker; b != nil {
		b.RecordFailure()
	}
}

func (l *AgentLoop) runMiddleware(ctx context.Context, phase middleware.Phase, sess *session.Session, t *tool.ToolSpec, input, result []byte) error {
	if l.Chain == nil {
		return nil
	}
	mc := &middleware.Context{
		Session:  sess,
		Tool:     t,
		Input:    input,
		Result:   result,
		IO:       l.IO,
		Observer: l.observer(),
	}
	return l.Chain.Run(ctx, phase, mc)
}

func (l *AgentLoop) runErrorMiddleware(ctx context.Context, sess *session.Session, err error) {
	if l.Chain == nil {
		return
	}
	mc := &middleware.Context{
		Session:  sess,
		Error:    err,
		IO:       l.IO,
		Observer: l.observer(),
	}
	l.Chain.Run(ctx, middleware.OnError, mc)
}

func (l *AgentLoop) emitLifecycle(ctx context.Context, event session.LifecycleEvent) {
	if l.LifecycleHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			sessionID := ""
			if event.Session != nil {
				sessionID = event.Session.ID
			}
			err := fmt.Errorf("session lifecycle hook panic: %v", r)
			slog.Default().ErrorContext(ctx, "session lifecycle hook panic",
				slog.String("stage", string(event.Stage)),
				slog.String("session_id", sessionID),
				slog.Any("panic", r),
			)
			port.ObserveError(context.Background(), l.observer(), port.ErrorEvent{
				SessionID: sessionID,
				Phase:     "session_lifecycle_hook",
				Error:     err,
				Message:   err.Error(),
			})
		}
	}()
	l.LifecycleHook(ctx, event)
}

func (l *AgentLoop) fail(ctx context.Context, sess *session.Session, usage port.TokenUsage, err error) *SessionResult {
	eventType := port.ExecutionRunFailed
	stage := session.LifecycleFailed
	if errors.Is(err, context.Canceled) || sess.Status == session.StatusCancelled {
		sess.Status = session.StatusCancelled
		eventType = port.ExecutionRunCancelled
		stage = session.LifecycleCancelled
	} else {
		sess.Status = session.StatusFailed
	}
	sess.EndedAt = time.Now()
	runEvent := l.executionEventBase(sess, eventType, "run", "runtime", "run")
	runEvent.Error = err.Error()
	runEvent.Data = map[string]any{
		"steps":  sess.Budget.UsedStepsValue(),
		"tokens": usage.TotalTokens,
	}
	appendExecutionErrorMetadata(&runEvent, err)
	port.ObserveExecutionEvent(context.Background(), l.observer(), runEvent)
	result := &SessionResult{
		SessionID:  sess.ID,
		Success:    false,
		Steps:      sess.Budget.UsedStepsValue(),
		TokensUsed: usage,
		Error:      err.Error(),
	}
	l.emitLifecycle(ctx, session.LifecycleEvent{
		Stage:   stage,
		Session: sess,
		Result: &session.LifecycleResult{
			Success:    false,
			Steps:      sess.Budget.UsedStepsValue(),
			TokensUsed: usage,
			Error:      err.Error(),
		},
		Error:     err,
		Timestamp: sess.EndedAt.UTC(),
	})
	return result
}
