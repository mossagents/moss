package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
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
	sidefxMu          sync.Mutex
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

// Run 执行 Agent Loop 直到完成、预算耗尽或达到最大迭代次数。
func (l *AgentLoop) Run(ctx context.Context, sess *session.Session) (*SessionResult, error) {
	sess.Status = session.StatusRunning

	runStartedAt := time.Now().UTC()
	l.beginRun(ctx, sess, runStartedAt)

	var lastOutput string
	var totalUsage port.TokenUsage
	maxIter := l.Config.maxIter()

	for i := 0; i < maxIter; i++ {
		if sess.Budget.Exhausted() {
			break
		}
		if ctx.Err() != nil {
			return l.fail(ctx, sess, totalUsage, ctx.Err()), ctx.Err()
		}
		shouldStop, err := l.runIteration(ctx, sess, i+1, maxIter, runStartedAt, &totalUsage, &lastOutput)
		if err != nil {
			return l.fail(ctx, sess, totalUsage, err), err
		}
		if shouldStop {
			break
		}
	}

	return l.completeRun(ctx, sess, totalUsage, lastOutput), nil
}

func (l *AgentLoop) beginRun(ctx context.Context, sess *session.Session, runStartedAt time.Time) {
	l.observer().OnSessionEvent(ctx, port.SessionEvent{SessionID: sess.ID, Type: "running"})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionRunStarted,
		SessionID: sess.ID,
		Timestamp: runStartedAt,
		Data: map[string]any{
			"mode":      sess.Config.Mode,
			"goal":      sess.Config.Goal,
			"max_steps": sess.Budget.MaxSteps,
		},
	})
	l.runMiddleware(ctx, middleware.OnSessionStart, sess, nil, nil, nil)
	l.emitLifecycle(ctx, session.LifecycleEvent{
		Stage:     session.LifecycleStarted,
		Session:   sess,
		Timestamp: runStartedAt,
	})
}

func (l *AgentLoop) runIteration(
	ctx context.Context,
	sess *session.Session,
	iteration int,
	maxIter int,
	runStartedAt time.Time,
	totalUsage *port.TokenUsage,
	lastOutput *string,
) (bool, error) {
	iterationStartedAt := time.Now().UTC()
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionIterationStarted,
		SessionID: sess.ID,
		Timestamp: iterationStartedAt,
		Data: map[string]any{
			"iteration":      iteration,
			"max_iterations": maxIter,
			"max_steps":      sess.Budget.MaxSteps,
			"elapsed_ms":     iterationStartedAt.Sub(runStartedAt).Milliseconds(),
		},
	})

	if err := l.runMiddleware(ctx, middleware.BeforeLLM, sess, nil, nil, nil); err != nil {
		return false, err
	}

	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionLLMStarted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Model:     sess.Config.ModelConfig.Model,
	})
	llmStart := time.Now()
	resp, streamed, err := l.callLLM(ctx, sess)
	llmDur := time.Since(llmStart)
	if err != nil {
		metadata := llmMetadataFromError(sess.Config.ModelConfig.Model, err)
		l.emitLLMAttemptEvents(ctx, sess.ID, metadata, true)
		l.observer().OnLLMCall(ctx, port.LLMCallEvent{
			SessionID: sess.ID, StartedAt: llmStart.UTC(), Duration: llmDur, Error: err, Streamed: streamed, Model: metadata.ActualModel,
		})
		l.observer().OnError(ctx, port.ErrorEvent{
			SessionID: sess.ID, Phase: "llm_call", Error: err, Message: err.Error(),
		})
		event := port.ExecutionEvent{
			Type:      port.ExecutionLLMCompleted,
			SessionID: sess.ID,
			Timestamp: time.Now().UTC(),
			Model:     metadata.ActualModel,
			Duration:  llmDur,
			Error:     err.Error(),
		}
		appendExecutionErrorMetadata(&event, err)
		l.observer().OnExecutionEvent(ctx, event)
		l.runErrorMiddleware(ctx, sess, err)
		return false, err
	}

	metadata := llmMetadataFromResponse(sess.Config.ModelConfig.Model, resp)
	l.emitLLMAttemptEvents(ctx, sess.ID, metadata, false)
	l.observer().OnLLMCall(ctx, port.LLMCallEvent{
		SessionID:  sess.ID,
		Model:      metadata.ActualModel,
		StartedAt:  llmStart.UTC(),
		Duration:   llmDur,
		Usage:      resp.Usage,
		StopReason: resp.StopReason,
		Streamed:   streamed,
	})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionLLMCompleted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Model:     metadata.ActualModel,
		Duration:  llmDur,
		Data: map[string]any{
			"stop_reason": resp.StopReason,
			"streamed":    streamed,
			"tokens":      resp.Usage.TotalTokens,
		},
	})

	totalUsage.PromptTokens += resp.Usage.PromptTokens
	totalUsage.CompletionTokens += resp.Usage.CompletionTokens
	totalUsage.TotalTokens += resp.Usage.TotalTokens
	if !sess.Budget.TryConsume(resp.Usage.TotalTokens, 1) {
		return true, nil
	}

	if err := l.runMiddleware(ctx, middleware.AfterLLM, sess, nil, nil, nil); err != nil {
		return false, err
	}

	sess.AppendMessage(resp.Message)

	if len(resp.ToolCalls) > 0 {
		if err := l.executeToolCalls(ctx, sess, resp.ToolCalls); err != nil {
			l.runErrorMiddleware(ctx, sess, err)
			return false, err
		}
	} else {
		*lastOutput = resp.Message.Content
		if l.IO != nil && !streamed {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputText,
				Content: resp.Message.Content,
			})
		}
	}

	progressAt := time.Now().UTC()
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionIterationProgress,
		SessionID: sess.ID,
		Timestamp: progressAt,
		Model:     metadata.ActualModel,
		Data: map[string]any{
			"iteration":      iteration,
			"max_iterations": maxIter,
			"max_steps":      sess.Budget.MaxSteps,
			"elapsed_ms":     progressAt.Sub(runStartedAt).Milliseconds(),
			"llm_calls":      1,
			"tool_calls":     len(resp.ToolCalls),
			"stop_reason":    resp.StopReason,
			"streamed":       streamed,
			"tokens":         resp.Usage.TotalTokens,
		},
	})

	if l.Config.StopWhen != nil && l.Config.StopWhen(resp.Message) {
		return true, nil
	}

	return resp.StopReason == "end_turn", nil
}

func (l *AgentLoop) completeRun(ctx context.Context, sess *session.Session, totalUsage port.TokenUsage, lastOutput string) *SessionResult {
	sess.Status = session.StatusCompleted
	sess.EndedAt = time.Now()
	l.observer().OnSessionEvent(ctx, port.SessionEvent{SessionID: sess.ID, Type: "completed"})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionRunCompleted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Data: map[string]any{
			"steps":  sess.Budget.UsedStepsValue(),
			"tokens": totalUsage.TotalTokens,
		},
	})
	l.runMiddleware(ctx, middleware.OnSessionEnd, sess, nil, nil, nil)
	result := &SessionResult{
		SessionID:  sess.ID,
		Success:    true,
		Output:     lastOutput,
		Steps:      sess.Budget.UsedStepsValue(),
		TokensUsed: totalUsage,
	}
	l.emitLifecycle(ctx, session.LifecycleEvent{
		Stage:   session.LifecycleCompleted,
		Session: sess,
		Result: &session.LifecycleResult{
			Success:    true,
			Output:     lastOutput,
			Steps:      sess.Budget.UsedStepsValue(),
			TokensUsed: totalUsage,
		},
		Timestamp: sess.EndedAt.UTC(),
	})
	return result
}

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session) (*port.CompletionResponse, bool, error) {
	specs := l.toolSpecs()
	req := port.CompletionRequest{
		Messages: sess.Messages,
		Tools:    specs,
		Config:   sess.Config.ModelConfig,
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
	var metadataProvider port.MetadataStreamIterator
	if provider, ok := iter.(port.MetadataStreamIterator); ok {
		metadataProvider = provider
	}

	var fullContent string
	var toolCalls []port.ToolCall
	var usage port.TokenUsage
	var stopReason string
	emittedContent := false

	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if emittedContent && len(toolCalls) > 0 && isRecoverableStreamTailError(err) {
				stopReason = "tool_use"
				if l.IO != nil {
					l.IO.Send(ctx, port.OutputMessage{Type: port.OutputStreamEnd})
				}
				break
			}
			safePreEmission := !emittedContent && len(toolCalls) == 0
			return nil, ensureLLMCallError(err, safePreEmission, safePreEmission, streamMetadata(metadataProvider))
		}

		if chunk.Delta != "" {
			emittedContent = true
			fullContent += chunk.Delta
			if l.IO != nil {
				l.IO.Send(ctx, port.OutputMessage{
					Type:    port.OutputStream,
					Content: chunk.Delta,
				})
			}
		}

		if chunk.ToolCall != nil {
			emittedContent = true
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}

		if chunk.Done {
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			stopReason = "end_turn"
			if len(toolCalls) > 0 {
				stopReason = "tool_use"
			}
			if l.IO != nil {
				l.IO.Send(ctx, port.OutputMessage{Type: port.OutputStreamEnd})
			}
			break
		}
	}

	msg := port.Message{
		Role:      port.RoleAssistant,
		Content:   fullContent,
		ToolCalls: toolCalls,
	}

	return &port.CompletionResponse{
		Message:    msg,
		ToolCalls:  toolCalls,
		Usage:      usage,
		StopReason: stopReason,
		Metadata:   metadataPtr(streamMetadata(metadataProvider)),
	}, nil
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
		l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
			Type:      port.ExecutionEventType("llm_failover_attempt"),
			SessionID: sessionID,
			Timestamp: time.Now().UTC(),
			Model:     attempt.CandidateModel,
			Data: map[string]any{
				"candidate_model": attempt.CandidateModel,
				"attempt_index":   attempt.AttemptIndex,
				"candidate_retry": attempt.CandidateRetry,
				"failure_reason":  attempt.FailureReason,
				"breaker_state":   attempt.BreakerState,
				"failover_to":     attempt.FailoverTo,
				"outcome":         attempt.Outcome,
			},
		})
		if strings.TrimSpace(attempt.FailoverTo) != "" {
			l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
				Type:      port.ExecutionEventType("llm_failover_switch"),
				SessionID: sessionID,
				Timestamp: time.Now().UTC(),
				Model:     attempt.CandidateModel,
				Data: map[string]any{
					"candidate_model": attempt.CandidateModel,
					"failover_to":     attempt.FailoverTo,
				},
			})
		}
	}
	if exhausted && len(metadata.Attempts) > 0 {
		l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
			Type:      port.ExecutionEventType("llm_failover_exhausted"),
			SessionID: sessionID,
			Timestamp: time.Now().UTC(),
			Model:     metadata.ActualModel,
		})
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
	spec, handler, ok := l.Tools.Get(call.Name)
	if !ok {
		return l.handleMissingTool(ctx, sess, call, repairedArgs)
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
			CallID:  callID,
			Content: err.Error(),
			IsError: true,
		}
	}
	return port.ToolResult{
		CallID:  callID,
		Content: string(output),
	}
}

func (l *AgentLoop) handleMissingTool(ctx context.Context, sess *session.Session, call port.ToolCall, repairedArgs json.RawMessage) port.ToolResult {
	err := fmt.Errorf("tool %q not found", call.Name)
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
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionToolStarted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		ToolName:  call.Name,
		CallID:    call.ID,
		Risk:      string(spec.Risk),
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
	l.observer().OnToolCall(ctx, port.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
		StartedAt: time.Now().UTC(),
		Duration:  0,
		Error:     normalizedErr,
	})
	event := port.ExecutionEvent{
		Type:      port.ExecutionToolCompleted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		ToolName:  call.Name,
		CallID:    call.ID,
		Risk:      string(spec.Risk),
		Data: map[string]any{
			"is_error": true,
		},
		Error: normalizedErr.Error(),
	}
	appendToolErrorMetadata(&event, normalizedErr)
	l.observer().OnExecutionEvent(ctx, event)
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
	l.observer().OnToolCall(ctx, port.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
		StartedAt: toolStart.UTC(),
		Duration:  toolDur,
		Error:     err,
	})
	event := port.ExecutionEvent{
		Type:      port.ExecutionToolCompleted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		ToolName:  call.Name,
		CallID:    call.ID,
		Risk:      string(spec.Risk),
		Duration:  toolDur,
		Data: map[string]any{
			"is_error": result.IsError,
		},
	}
	if err != nil {
		event.Error = err.Error()
		appendToolErrorMetadata(&event, err)
	}
	appendToolExecutionMetadata(&event, output)
	l.observer().OnExecutionEvent(ctx, event)
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
		Content: result.Content,
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
			l.observer().OnError(context.Background(), port.ErrorEvent{
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

func repairToolArguments(args json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	repaired := repairTruncatedJSON(trimmed)
	if json.Valid([]byte(repaired)) {
		return json.RawMessage(repaired)
	}
	return args
}

func previewToolArguments(args json.RawMessage) string {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "{}" {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err == nil {
		trimmed = compact.String()
	}
	if len(trimmed) > 160 {
		return trimmed[:160] + "..."
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
		escaped = false
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

func (l *AgentLoop) withSideEffectsLock(fn func()) {
	l.sidefxMu.Lock()
	defer l.sidefxMu.Unlock()
	fn()
}

func (l *AgentLoop) toolSpecs() []port.ToolSpec {
	tools := l.Tools.List()
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
			l.observer().OnError(context.Background(), port.ErrorEvent{
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
	runEvent := port.ExecutionEvent{
		Type:      eventType,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Error:     err.Error(),
		Data: map[string]any{
			"steps":  sess.Budget.UsedStepsValue(),
			"tokens": usage.TotalTokens,
		},
	}
	appendExecutionErrorMetadata(&runEvent, err)
	l.observer().OnExecutionEvent(context.Background(), runEvent)
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
