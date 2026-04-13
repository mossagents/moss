package loop

import (
	"context"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

// Run 执行 Agent Loop 直到完成、预算耗尽或达到最大迭代次数。
func (l *AgentLoop) Run(ctx context.Context, sess *session.Session) (*SessionResult, error) {
	return l.runCore(ctx, sess)
}

// RunYield executes the Agent Loop and yields events in real-time via the yield callback.
// Events are yielded at two key points in each iteration:
//   - After the LLM response (EventTypeLLMResponse): includes assistant message and token usage
//   - After tool execution (EventTypeToolResult): includes tool results
//
// If yield returns false, the loop stops gracefully.
// Errors are returned (not yielded); the caller should yield errors separately if needed.
func (l *AgentLoop) RunYield(ctx context.Context, sess *session.Session, yield func(*session.Event, error) bool) (*SessionResult, error) {
	l.eventYield = yield
	return l.runCore(ctx, sess)
}

func (l *AgentLoop) runCore(ctx context.Context, sess *session.Session) (*SessionResult, error) {
	sess.Status = session.StatusRunning

	// 若配置了 ContextCompression 策略，在运行前注入压缩 hook。
	l.injectCompressionHooks()

	runStartedAt := time.Now().UTC()
	l.beginRun(ctx, sess, runStartedAt)

	var lastOutput string
	var totalUsage model.TokenUsage
	maxIter := l.Config.maxIter()

	for i := 0; i < maxIter; i++ {
		if l.yieldStopped {
			break
		}
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
	observe.ObserveSessionEvent(ctx, l.observer(), observe.SessionEvent{SessionID: sess.ID, Type: "running"})
	event := l.executionEventBase(sess, observe.ExecutionRunStarted, "run", "runtime", "run")
	event.Timestamp = runStartedAt
	event.Metadata = map[string]any{
		"mode":      sess.Config.Mode,
		"goal":      sess.Config.Goal,
		"max_steps": sess.Budget.MaxSteps,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
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
	totalUsage *model.TokenUsage,
	lastOutput *string,
) (bool, error) {
	l.currentTurn = buildTurnPlan(sess, l.RunID, iteration, l.Tools)
	l.persistTurnMetadata(sess, l.currentTurn)
	l.emitIterationStart(ctx, sess, iteration, maxIter, runStartedAt)
	l.emitTurnPlanEvents(ctx, sess, l.currentTurn)
	llmResult, err := l.executeIterationLLM(ctx, sess, l.currentTurn)
	if err != nil {
		return false, err
	}
	resp := llmResult.resp
	metadata := llmResult.metadata

	totalUsage.PromptTokens += resp.Usage.PromptTokens
	totalUsage.CompletionTokens += resp.Usage.CompletionTokens
	totalUsage.TotalTokens += resp.Usage.TotalTokens
	if !sess.Budget.TryConsume(resp.Usage.TotalTokens, 1) {
		return true, nil
	}

	if err := l.safeHooks().AfterLLM.Run(ctx, &hooks.LLMEvent{Session: sess, IO: l.IO, Observer: l.observer()}); err != nil {
		return false, err
	}

	sess.AppendMessage(resp.Message)

	// Yield LLM response event in real-time.
	event := &session.Event{
		Type:      session.EventTypeLLMResponse,
		Author:    l.AgentName,
		Content:   &resp.Message,
		Usage:     resp.Usage,
		TurnID:    l.currentTurn.TurnID,
		Timestamp: time.Now().UTC(),
	}
	session.MarkEventMaterialized(event, sess)
	if !l.emitAgentEvent(event) {
		return true, nil // consumer stopped iteration
	}

	if err := l.processIterationResponse(ctx, sess, resp, lastOutput); err != nil {
		return false, err
	}

	l.emitIterationProgress(ctx, sess, iteration, maxIter, runStartedAt, metadata.ActualModel, resp)

	if l.Config.StopWhen != nil && l.Config.StopWhen(resp.Message) {
		return true, nil
	}

	return resp.StopReason == "end_turn", nil
}

type iterationLLMResult struct {
	resp     *model.CompletionResponse
	metadata model.LLMCallMetadata
}

func (l *AgentLoop) emitIterationStart(ctx context.Context, sess *session.Session, iteration, maxIter int, runStartedAt time.Time) time.Time {
	iterationStartedAt := time.Now().UTC()
	logging.GetLogger().DebugContext(ctx, "iteration started",
		"session_id", sess.ID,
		"turn_id", l.currentTurn.TurnID,
		"iteration", iteration,
		"max_iterations", maxIter,
		"elapsed_ms", iterationStartedAt.Sub(runStartedAt).Milliseconds(),
	)
	event := l.executionEventBase(sess, observe.ExecutionIterationStarted, "iteration", "runtime", "iteration")
	event.Timestamp = iterationStartedAt
	event.Metadata = map[string]any{
		"iteration":      iteration,
		"max_iterations": maxIter,
		"max_steps":      sess.Budget.MaxSteps,
		"elapsed_ms":     iterationStartedAt.Sub(runStartedAt).Milliseconds(),
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
	return iterationStartedAt
}

func (l *AgentLoop) executeIterationLLM(ctx context.Context, sess *session.Session, plan TurnPlan) (*iterationLLMResult, error) {
	if err := l.safeHooks().BeforeLLM.Run(ctx, &hooks.LLMEvent{Session: sess, IO: l.IO, Observer: l.observer()}); err != nil {
		return nil, err
	}

	event := l.executionEventBase(sess, observe.ExecutionLLMStarted, "llm", "runtime", "llm")
	event.Model = sess.Config.ModelConfig.Model
	event.Metadata = map[string]any{
		"model_lane":          plan.ModelRoute.Lane,
		"instruction_profile": plan.InstructionProfile,
		"prompt_version":      plan.PromptVersion,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
	llmStart := time.Now()
	resp, err := l.callLLM(ctx, sess, plan)
	llmDur := time.Since(llmStart)
	if err != nil {
		metadata := llmMetadataFromError(sess.Config.ModelConfig.Model, err)
		logging.GetLogger().DebugContext(ctx, "llm response failed",
			"session_id", sess.ID,
			"model", metadata.ActualModel,
			"duration_ms", llmDur.Milliseconds(),
			"error", err.Error(),
		)
		l.emitLLMAttemptEvents(ctx, sess.ID, metadata, true)
		observe.ObserveLLMCall(ctx, l.observer(), observe.LLMCallEvent{
			SessionID: sess.ID, StartedAt: llmStart.UTC(), Duration: llmDur, Error: err, Streamed: true, Model: metadata.ActualModel,
		})
		observe.ObserveError(ctx, l.observer(), observe.ErrorEvent{
			SessionID: sess.ID, Phase: "llm_call", Error: err, Message: err.Error(),
		})
		event := l.executionEventBase(sess, observe.ExecutionLLMCompleted, "llm", "runtime", "llm")
		event.Model = metadata.ActualModel
		event.Duration = llmDur
		event.Error = err.Error()
		appendExecutionErrorMetadata(&event, err)
		observe.ObserveExecutionEvent(ctx, l.observer(), event)
		l.runErrorHook(ctx, sess, err)
		return nil, err
	}

	metadata := llmMetadataFromResponse(sess.Config.ModelConfig.Model, resp)
	logging.GetLogger().DebugContext(ctx, "llm response received",
		"session_id", sess.ID,
		"model", metadata.ActualModel,
		"duration_ms", llmDur.Milliseconds(),
		"stop_reason", resp.StopReason,
		"tool_calls", len(resp.ToolCalls),
		"tokens", resp.Usage.TotalTokens,
	)
	l.emitLLMAttemptEvents(ctx, sess.ID, metadata, false)
	observe.ObserveLLMCall(ctx, l.observer(), observe.LLMCallEvent{
		SessionID:  sess.ID,
		Model:      metadata.ActualModel,
		StartedAt:  llmStart.UTC(),
		Duration:   llmDur,
		Usage:      resp.Usage,
		StopReason: resp.StopReason,
		Streamed:   true,
	})
	event = l.executionEventBase(sess, observe.ExecutionLLMCompleted, "llm", "runtime", "llm")
	event.Model = metadata.ActualModel
	event.Duration = llmDur
	event.Metadata = map[string]any{
		"stop_reason":         resp.StopReason,
		"tokens":              resp.Usage.TotalTokens,
		"model_lane":          plan.ModelRoute.Lane,
		"instruction_profile": plan.InstructionProfile,
		"prompt_version":      plan.PromptVersion,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)

	return &iterationLLMResult{resp: resp, metadata: metadata}, nil
}

func (l *AgentLoop) persistTurnMetadata(sess *session.Session, plan TurnPlan) {
	if sess == nil {
		return
	}
	sess.SetMetadataBatch(map[string]any{
		session.MetadataRunID:              plan.RunID,
		session.MetadataTurnID:             plan.TurnID,
		session.MetadataInstructionProfile: plan.InstructionProfile,
		session.MetadataPromptVersion:      plan.PromptVersion,
		session.MetadataModelLane:          plan.ModelRoute.Lane,
		session.MetadataVisibleTools:       visibleToolNames(plan.ToolRoute),
		session.MetadataHiddenTools:        hiddenToolNames(plan.ToolRoute),
		session.MetadataToolRouteDigest:    toolRouteDigest(plan.ToolRoute),
	})
}

func (l *AgentLoop) emitTurnPlanEvents(ctx context.Context, sess *session.Session, plan TurnPlan) {
	routeEvent := l.executionEventBase(sess, observe.ExecutionEventType("tool.route_planned"), "planning", "runtime", "tool_route")
	visible := visibleToolNames(plan.ToolRoute)
	hidden := hiddenToolNames(plan.ToolRoute)
	approval := approvalRequiredToolNames(plan.ToolRoute)
	routeEvent.Metadata = map[string]any{
		"visible_tools":        visible,
		"hidden_tools":         hidden,
		"approval_tools":       approval,
		"visible_tools_count":  len(visible),
		"hidden_tools_count":   len(hidden),
		"approval_tools_count": len(approval),
		"route_digest":         toolRouteDigest(plan.ToolRoute),
		"decisions":            toolRoutePayload(plan.ToolRoute),
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), routeEvent)

	modelEvent := l.executionEventBase(sess, observe.ExecutionEventType("model.route_planned"), "planning", "runtime", "model_route")
	modelEvent.Model = sess.Config.ModelConfig.Model
	modelEvent.Metadata = map[string]any{
		"lane":          plan.ModelRoute.Lane,
		"reason_codes":  append([]string(nil), plan.ModelRoute.ReasonCodes...),
		"capabilities":  append([]model.ModelCapability(nil), plan.ModelRoute.Requirements.Capabilities...),
		"max_cost_tier": plan.ModelRoute.Requirements.MaxCostTier,
		"prefer_cheap":  plan.ModelRoute.Requirements.PreferCheap,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), modelEvent)

	turnEvent := l.executionEventBase(sess, observe.ExecutionEventType("turn.plan_prepared"), "planning", "runtime", "turn_plan")
	turnEvent.Metadata = map[string]any{
		"iteration":            plan.Iteration,
		"instruction_profile":  plan.InstructionProfile,
		"prompt_version":       plan.PromptVersion,
		"lightweight_chat":     plan.LightweightChat,
		"visible_tools_count":  len(visible),
		"hidden_tools_count":   len(hidden),
		"approval_tools_count": len(approval),
		"model_lane":           plan.ModelRoute.Lane,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), turnEvent)
}

func (l *AgentLoop) processIterationResponse(ctx context.Context, sess *session.Session, resp *model.CompletionResponse, lastOutput *string) error {
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
			l.runErrorHook(ctx, sess, err)
			return err
		}
		return nil
	}

	*lastOutput = model.ContentPartsToPlainText(resp.Message.ContentParts)
	logging.GetLogger().DebugContext(ctx, "assistant produced final content",
		"session_id", sess.ID,
		"chars", len(*lastOutput),
	)
	return nil
}

func (l *AgentLoop) emitIterationProgress(
	ctx context.Context,
	sess *session.Session,
	iteration int,
	maxIter int,
	runStartedAt time.Time,
	modelName string,
	resp *model.CompletionResponse,
) {
	progressAt := time.Now().UTC()
	event := l.executionEventBase(sess, observe.ExecutionIterationProgress, "iteration", "runtime", "iteration")
	event.Timestamp = progressAt
	event.Model = modelName
	event.Metadata = map[string]any{
		"iteration":      iteration,
		"max_iterations": maxIter,
		"max_steps":      sess.Budget.MaxSteps,
		"elapsed_ms":     progressAt.Sub(runStartedAt).Milliseconds(),
		"llm_calls":      1,
		"tool_calls":     len(resp.ToolCalls),
		"stop_reason":    resp.StopReason,
		"tokens":         resp.Usage.TotalTokens,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) completeRun(ctx context.Context, sess *session.Session, totalUsage model.TokenUsage, lastOutput string) *SessionResult {
	sess.Status = session.StatusCompleted
	sess.EndedAt = time.Now()
	logging.GetLogger().DebugContext(ctx, "session run completed",
		"session_id", sess.ID,
		"steps", sess.Budget.UsedStepsValue(),
		"tokens", totalUsage.TotalTokens,
	)
	observe.ObserveSessionEvent(ctx, l.observer(), observe.SessionEvent{SessionID: sess.ID, Type: "completed"})
	event := l.executionEventBase(sess, observe.ExecutionRunCompleted, "run", "runtime", "run")
	event.Metadata = map[string]any{
		"steps":  sess.Budget.UsedStepsValue(),
		"tokens": totalUsage.TotalTokens,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
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
