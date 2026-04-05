package loop

import (
	"context"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
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
	logging.GetLogger().DebugContext(ctx, "session run started",
		"session_id", sess.ID,
		"mode", sess.Config.Mode,
		"goal_chars", len(sess.Config.Goal),
		"max_steps", sess.Budget.MaxSteps,
	)
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
	l.emitIterationStart(ctx, sess, iteration, maxIter, runStartedAt)
	llmResult, err := l.executeIterationLLM(ctx, sess)
	if err != nil {
		return false, err
	}
	resp := llmResult.resp
	streamed := llmResult.streamed
	metadata := llmResult.metadata

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

	if err := l.processIterationResponse(ctx, sess, resp, streamed, lastOutput); err != nil {
		return false, err
	}

	l.emitIterationProgress(ctx, sess, iteration, maxIter, runStartedAt, metadata.ActualModel, streamed, resp)

	if l.Config.StopWhen != nil && l.Config.StopWhen(resp.Message) {
		return true, nil
	}

	return resp.StopReason == "end_turn", nil
}

type iterationLLMResult struct {
	resp     *port.CompletionResponse
	streamed bool
	metadata port.LLMCallMetadata
}

func (l *AgentLoop) emitIterationStart(ctx context.Context, sess *session.Session, iteration, maxIter int, runStartedAt time.Time) time.Time {
	iterationStartedAt := time.Now().UTC()
	logging.GetLogger().DebugContext(ctx, "iteration started",
		"session_id", sess.ID,
		"iteration", iteration,
		"max_iterations", maxIter,
		"elapsed_ms", iterationStartedAt.Sub(runStartedAt).Milliseconds(),
	)
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
	return iterationStartedAt
}

func (l *AgentLoop) executeIterationLLM(ctx context.Context, sess *session.Session) (*iterationLLMResult, error) {
	if err := l.runMiddleware(ctx, middleware.BeforeLLM, sess, nil, nil, nil); err != nil {
		return nil, err
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
		logging.GetLogger().DebugContext(ctx, "llm response failed",
			"session_id", sess.ID,
			"model", metadata.ActualModel,
			"streamed", streamed,
			"duration_ms", llmDur.Milliseconds(),
			"error", err.Error(),
		)
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
		return nil, err
	}

	metadata := llmMetadataFromResponse(sess.Config.ModelConfig.Model, resp)
	logging.GetLogger().DebugContext(ctx, "llm response received",
		"session_id", sess.ID,
		"model", metadata.ActualModel,
		"streamed", streamed,
		"duration_ms", llmDur.Milliseconds(),
		"stop_reason", resp.StopReason,
		"tool_calls", len(resp.ToolCalls),
		"tokens", resp.Usage.TotalTokens,
	)
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

	return &iterationLLMResult{resp: resp, streamed: streamed, metadata: metadata}, nil
}

func (l *AgentLoop) processIterationResponse(ctx context.Context, sess *session.Session, resp *port.CompletionResponse, streamed bool, lastOutput *string) error {
	if len(resp.ToolCalls) > 0 {
		names := make([]string, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			names = append(names, call.Name)
		}
		logging.GetLogger().DebugContext(ctx, "assistant requested tool calls",
			"session_id", sess.ID,
			"tool_calls", strings.Join(names, ","),
		)
		if err := l.executeToolCalls(ctx, sess, resp.ToolCalls); err != nil {
			l.runErrorMiddleware(ctx, sess, err)
			return err
		}
		return nil
	}

	*lastOutput = port.ContentPartsToPlainText(resp.Message.ContentParts)
	logging.GetLogger().DebugContext(ctx, "assistant produced final content",
		"session_id", sess.ID,
		"streamed", streamed,
		"chars", len(*lastOutput),
	)
	if l.IO != nil && !streamed {
		for _, part := range resp.Message.ContentParts {
			if part.Type != port.ContentPartReasoning || strings.TrimSpace(part.Text) == "" {
				continue
			}
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputReasoning,
				Content: part.Text,
			})
		}
		l.IO.Send(ctx, port.OutputMessage{
			Type:    port.OutputText,
			Content: *lastOutput,
		})
	}
	return nil
}

func (l *AgentLoop) emitIterationProgress(
	ctx context.Context,
	sess *session.Session,
	iteration int,
	maxIter int,
	runStartedAt time.Time,
	model string,
	streamed bool,
	resp *port.CompletionResponse,
) {
	progressAt := time.Now().UTC()
	l.observer().OnExecutionEvent(ctx, port.ExecutionEvent{
		Type:      port.ExecutionIterationProgress,
		SessionID: sess.ID,
		Timestamp: progressAt,
		Model:     model,
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
}

func (l *AgentLoop) completeRun(ctx context.Context, sess *session.Session, totalUsage port.TokenUsage, lastOutput string) *SessionResult {
	sess.Status = session.StatusCompleted
	sess.EndedAt = time.Now()
	logging.GetLogger().DebugContext(ctx, "session run completed",
		"session_id", sess.ID,
		"steps", sess.Budget.UsedStepsValue(),
		"tokens", totalUsage.TotalTokens,
	)
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
