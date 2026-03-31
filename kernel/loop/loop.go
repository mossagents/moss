package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	MaxIterations    int                     // 最大循环次数（默认 50）
	StopWhen         func(port.Message) bool // 自定义停止条件
	ParallelToolCall bool                    // 启用并行工具调用（默认 false，串行执行）
	LLMRetry         RetryConfig             // LLM 调用重试配置
	LLMBreaker       *retry.Breaker          // LLM 调用熔断器（可选）
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
	LLM      port.LLM
	Tools    tool.Registry
	Chain    *middleware.Chain
	IO       port.UserIO
	Config   LoopConfig
	Observer port.Observer // 可观测性观察者（可选，默认 NoOpObserver）
	sidefxMu sync.Mutex
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

	// OnSessionStart
	l.observer().OnSessionEvent(ctx, port.SessionEvent{SessionID: sess.ID, Type: "running"})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionRunStarted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Data: map[string]any{
			"mode": sess.Config.Mode,
			"goal": sess.Config.Goal,
		},
	})
	l.runMiddleware(ctx, middleware.OnSessionStart, sess, nil, nil, nil)

	var lastOutput string
	var totalUsage port.TokenUsage
	maxIter := l.Config.maxIter()

	for i := 0; i < maxIter; i++ {
		if sess.Budget.Exhausted() {
			break
		}
		if ctx.Err() != nil {
			return l.fail(sess, totalUsage, ctx.Err()), ctx.Err()
		}

		// 1. BeforeLLM middleware
		if err := l.runMiddleware(ctx, middleware.BeforeLLM, sess, nil, nil, nil); err != nil {
			return l.fail(sess, totalUsage, err), err
		}

		// 2. LLM 调用
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
				SessionID: sess.ID, Duration: llmDur, Error: err, Streamed: streamed, Model: metadata.ActualModel,
			})
			l.observer().OnError(ctx, port.ErrorEvent{
				SessionID: sess.ID, Phase: "llm_call", Error: err, Message: err.Error(),
			})
			l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
				Type:      port.ExecutionLLMCompleted,
				SessionID: sess.ID,
				Timestamp: time.Now().UTC(),
				Model:     metadata.ActualModel,
				Duration:  llmDur,
				Error:     err.Error(),
			})
			l.runErrorMiddleware(ctx, sess, err)
			return l.fail(sess, totalUsage, err), err
		}

		metadata := llmMetadataFromResponse(sess.Config.ModelConfig.Model, resp)
		l.emitLLMAttemptEvents(ctx, sess.ID, metadata, false)
		l.observer().OnLLMCall(ctx, port.LLMCallEvent{
			SessionID:  sess.ID,
			Model:      metadata.ActualModel,
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
		sess.Budget.Record(resp.Usage.TotalTokens, 1)

		// 3. AfterLLM middleware
		if err := l.runMiddleware(ctx, middleware.AfterLLM, sess, nil, nil, nil); err != nil {
			return l.fail(sess, totalUsage, err), err
		}

		// 4. 追加 assistant 消息
		sess.AppendMessage(resp.Message)

		// 5. 处理 tool calls 或文本回复
		if len(resp.ToolCalls) > 0 {
			if err := l.executeToolCalls(ctx, sess, resp.ToolCalls); err != nil {
				l.runErrorMiddleware(ctx, sess, err)
				return l.fail(sess, totalUsage, err), err
			}
		} else {
			lastOutput = resp.Message.Content
			// 流式模式下 IO 已经逐 chunk 输出过了，不再重复发送。
			if l.IO != nil && !streamed {
				l.IO.Send(ctx, port.OutputMessage{
					Type:    port.OutputText,
					Content: resp.Message.Content,
				})
			}

			// 自定义停止条件
			if l.Config.StopWhen != nil && l.Config.StopWhen(resp.Message) {
				break
			}

			// end_turn 停止
			if resp.StopReason == "end_turn" {
				break
			}
		}
	}

	sess.Status = session.StatusCompleted
	sess.EndedAt = time.Now()
	l.observer().OnSessionEvent(ctx, port.SessionEvent{SessionID: sess.ID, Type: "completed"})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionRunCompleted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Data: map[string]any{
			"steps":  sess.Budget.UsedSteps,
			"tokens": totalUsage.TotalTokens,
		},
	})
	l.runMiddleware(ctx, middleware.OnSessionEnd, sess, nil, nil, nil)

	return &SessionResult{
		SessionID:  sess.ID,
		Success:    true,
		Output:     lastOutput,
		Steps:      sess.Budget.UsedSteps,
		TokensUsed: totalUsage,
	}, nil
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
		if !attempt.retryable || !cfg.ShouldRetryOrDefault(attempt.err) || attemptIndex == maxRetries {
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
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected end of json input") || strings.Contains(msg, "unexpected eof")
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

func (l *AgentLoop) executeToolCallsParallel(ctx context.Context, sess *session.Session, calls []port.ToolCall) error {
	results := make([]port.ToolResult, len(calls))

	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c port.ToolCall) {
			defer wg.Done()
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
	spec, handler, ok := l.Tools.Get(call.Name)
	if !ok {
		return port.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("tool %q not found", call.Name),
			IsError: true,
		}
	}
	repairedArgs := repairToolArguments(call.Arguments)

	// UserIO: 通知工具开始
	l.withSideEffectsLock(func() {
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
	})
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionToolStarted,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		ToolName:  call.Name,
		CallID:    call.ID,
		Risk:      string(spec.Risk),
	})

	// BeforeToolCall middleware
	var beforeErr error
	l.withSideEffectsLock(func() {
		beforeErr = l.runMiddleware(ctx, middleware.BeforeToolCall, sess, &spec, call.Arguments, nil)
	})
	if beforeErr != nil {
		return port.ToolResult{
			CallID:  call.ID,
			Content: beforeErr.Error(),
			IsError: true,
		}
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

	var result port.ToolResult
	if err != nil {
		result = port.ToolResult{
			CallID:  call.ID,
			Content: err.Error(),
			IsError: true,
		}
	} else {
		result = port.ToolResult{
			CallID:  call.ID,
			Content: string(output),
		}
	}

	// Observer: 工具调用指标
	l.observer().OnToolCall(ctx, port.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
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
	}
	appendToolExecutionMetadata(&event, output)
	l.observer().OnExecutionEvent(ctx, event)

	// AfterToolCall middleware
	l.withSideEffectsLock(func() {
		l.runMiddleware(ctx, middleware.AfterToolCall, sess, &spec, nil, output)
	})

	// UserIO: 通知工具结果
	l.withSideEffectsLock(func() {
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputToolResult,
				Content: result.Content,
				Meta: map[string]any{
					"call_id":     call.ID,
					"tool":        call.Name,
					"is_error":    result.IsError,
					"duration_ms": toolDur.Milliseconds(),
				},
			})
		}
	})

	return result
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

func (l *AgentLoop) fail(sess *session.Session, usage port.TokenUsage, err error) *SessionResult {
	eventType := port.ExecutionRunFailed
	if errors.Is(err, context.Canceled) || sess.Status == session.StatusCancelled {
		sess.Status = session.StatusCancelled
		eventType = port.ExecutionRunCancelled
	} else {
		sess.Status = session.StatusFailed
	}
	sess.EndedAt = time.Now()
	l.observer().OnExecutionEvent(context.Background(), port.ExecutionEvent{
		Type:      eventType,
		SessionID: sess.ID,
		Timestamp: time.Now().UTC(),
		Error:     err.Error(),
		Data: map[string]any{
			"steps":  sess.Budget.UsedSteps,
			"tokens": usage.TotalTokens,
		},
	})
	return &SessionResult{
		SessionID:  sess.ID,
		Success:    false,
		Steps:      sess.Budget.UsedSteps,
		TokensUsed: usage,
		Error:      err.Error(),
	}
}
