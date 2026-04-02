package loop

import (
	"context"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

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
