package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
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

type retryableCallError struct {
	err       error
	retryable bool
}

func (e *retryableCallError) Error() string {
	return e.err.Error()
}

func (e *retryableCallError) Unwrap() error {
	return e.err
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
		llmStart := time.Now()
		resp, streamed, err := l.callLLM(ctx, sess)
		llmDur := time.Since(llmStart)
		if err != nil {
			l.observer().OnLLMCall(ctx, port.LLMCallEvent{
				SessionID: sess.ID, Duration: llmDur, Error: err, Streamed: streamed,
			})
			l.observer().OnError(ctx, port.ErrorEvent{
				SessionID: sess.ID, Phase: "llm_call", Error: err, Message: err.Error(),
			})
			l.runErrorMiddleware(ctx, sess, err)
			return l.fail(sess, totalUsage, err), err
		}

		l.observer().OnLLMCall(ctx, port.LLMCallEvent{
			SessionID:  sess.ID,
			Duration:   llmDur,
			Usage:      resp.Usage,
			StopReason: resp.StopReason,
			Streamed:   streamed,
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
				err:       kerrors.New(kerrors.ErrLLMRejected, "LLM circuit breaker is open: too many recent failures"),
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

		l.recordBreakerFailure()
		var streamErr *retryableCallError
		if errors.As(err, &streamErr) {
			return callAttemptResult{streamed: true, retryable: streamErr.retryable, err: err}
		}
		return callAttemptResult{streamed: true, retryable: true, err: err}
	}

	resp, err := l.LLM.Complete(ctx, req)
	if err != nil {
		l.recordBreakerFailure()
	} else {
		l.recordBreakerSuccess()
	}
	return callAttemptResult{resp: resp, streamed: false, retryable: true, err: err}
}

func (l *AgentLoop) streamLLM(ctx context.Context, sllm port.StreamingLLM, req port.CompletionRequest) (*port.CompletionResponse, error) {
	iter, err := sllm.Stream(ctx, req)
	if err != nil {
		return nil, &retryableCallError{err: err, retryable: true}
	}
	defer iter.Close()

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
			return nil, &retryableCallError{err: err, retryable: !emittedContent}
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
	}, nil
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

	// UserIO: 通知工具开始
	l.withSideEffectsLock(func() {
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputToolStart,
				Content: call.Name,
				Meta:    map[string]any{"call_id": call.ID},
			})
		}
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
	output, err := handler(toolCtx, call.Arguments)
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
				Meta:    map[string]any{"call_id": call.ID, "tool": call.Name, "is_error": result.IsError},
			})
		}
	})

	return result
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
		Session: sess,
		Tool:    t,
		Input:   input,
		Result:  result,
		IO:      l.IO,
	}
	return l.Chain.Run(ctx, phase, mc)
}

func (l *AgentLoop) runErrorMiddleware(ctx context.Context, sess *session.Session, err error) {
	if l.Chain == nil {
		return
	}
	mc := &middleware.Context{
		Session: sess,
		Error:   err,
		IO:      l.IO,
	}
	l.Chain.Run(ctx, middleware.OnError, mc)
}

func (l *AgentLoop) fail(sess *session.Session, usage port.TokenUsage, err error) *SessionResult {
	if errors.Is(err, context.Canceled) || sess.Status == session.StatusCancelled {
		sess.Status = session.StatusCancelled
	} else {
		sess.Status = session.StatusFailed
	}
	sess.EndedAt = time.Now()
	return &SessionResult{
		SessionID:  sess.ID,
		Success:    false,
		Steps:      sess.Budget.UsedSteps,
		TokensUsed: usage,
		Error:      err.Error(),
	}
}
