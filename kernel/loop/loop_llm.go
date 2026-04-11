package loop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session, plan TurnPlan) (*model.CompletionResponse, error) {
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
	req := model.CompletionRequest{
		Messages: promptMessages,
		Tools:    specs,
		Config:   modelConfig,
	}

	cfg := l.Config.LLMRetry
	if !cfg.Enabled() {
		attempt := l.callLLMOnce(ctx, req)
		return attempt.resp, attempt.err
	}

	maxRetries := cfg.MaxRetriesOrDefault()
	delay := cfg.InitialDelayOrDefault()
	var lastErr error

	for attemptIndex := 0; attemptIndex <= maxRetries; attemptIndex++ {
		attempt := l.callLLMOnce(ctx, req)
		if attempt.err == nil {
			return attempt.resp, nil
		}

		lastErr = attempt.err
		if !attempt.retryable || !cfg.ShouldRetryOrDefault(ctx, attempt.err) || attemptIndex == maxRetries {
			return nil, attempt.err
		}

		jitter := time.Duration(rand.Int63n(int64(delay) / 2))
		sleepDuration := delay + jitter
		if sleepDuration > cfg.MaxDelayOrDefault() {
			sleepDuration = cfg.MaxDelayOrDefault()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleepDuration):
		}

		delay = time.Duration(float64(delay) * cfg.MultiplierOrDefault())
		if delay > cfg.MaxDelayOrDefault() {
			delay = cfg.MaxDelayOrDefault()
		}
	}

	return nil, lastErr
}

func (l *AgentLoop) callLLMOnce(ctx context.Context, req model.CompletionRequest) callAttemptResult {
	// 熔断器检查
	if b := l.Config.LLMBreaker; b != nil {
		if !b.Allow() {
			return callAttemptResult{
				err: &model.LLMCallError{
					Err:       kerrors.New(kerrors.ErrLLMRejected, "LLM circuit breaker is open: too many recent failures"),
					Retryable: false,
				},
				retryable: false,
			}
		}
	}

	state := streamAccumulator{}
	var metadata *model.LLMCallMetadata

	for chunk, err := range l.LLM.GenerateContent(ctx, req) {
		if err != nil {
			// Check recoverable tail errors when we have partial content + tool calls.
			if state.emittedContent && len(state.toolCalls) > 0 && isRecoverableStreamTailError(err) {
				state.stopReason = "tool_use"
				if l.IO != nil {
					if sendErr := l.IO.Send(ctx, kernio.OutputMessage{Type: kernio.OutputStreamEnd}); sendErr != nil {
						logging.GetLogger().DebugContext(ctx, "stream end send failed", "error", sendErr)
					}
				}
				break
			}
			safePreEmission := !state.emittedContent && len(state.toolCalls) == 0
			llmErr := ensureLLMCallError(err, safePreEmission, safePreEmission, derefMetadata(metadata))
			l.recordBreakerFailure()
			return callAttemptResult{retryable: llmErrorRetryable(llmErr), err: llmErr}
		}

		// Extract metadata from chunks (typically on the Done chunk).
		if chunk.Metadata != nil {
			metadata = chunk.Metadata
		}
		if done := l.applyStreamChunk(ctx, chunk, &state); done {
			break
		}
	}

	l.recordBreakerSuccess()

	msg := model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: state.toolCalls,
	}
	if state.fullReasoning != "" {
		msg.ContentParts = append(msg.ContentParts, model.ReasoningPart(state.fullReasoning))
	}
	if state.fullContent != "" {
		msg.ContentParts = append(msg.ContentParts, model.TextPart(state.fullContent))
	}

	return callAttemptResult{
		resp: &model.CompletionResponse{
			Message:    msg,
			ToolCalls:  state.toolCalls,
			Usage:      state.usage,
			StopReason: state.stopReason,
			Metadata:   metadata,
		},
	}
}

type streamAccumulator struct {
	fullContent    string
	fullReasoning  string
	toolCalls      []model.ToolCall
	usage          model.TokenUsage
	stopReason     string
	emittedContent bool
}

func derefMetadata(m *model.LLMCallMetadata) model.LLMCallMetadata {
	if m == nil {
		return model.LLMCallMetadata{}
	}
	return *m
}

func (l *AgentLoop) applyStreamChunk(ctx context.Context, chunk model.StreamChunk, state *streamAccumulator) bool {
	if chunk.ReasoningDelta != "" {
		state.emittedContent = true
		state.fullReasoning += chunk.ReasoningDelta
		if l.IO != nil {
			if err := l.IO.Send(ctx, kernio.OutputMessage{
				Type:    kernio.OutputReasoning,
				Content: chunk.ReasoningDelta,
			}); err != nil {
				logging.GetLogger().DebugContext(ctx, "reasoning send failed", "error", err)
			}
		}
	}

	if chunk.Delta != "" {
		state.emittedContent = true
		state.fullContent += chunk.Delta
		if l.IO != nil {
			if err := l.IO.Send(ctx, kernio.OutputMessage{
				Type:    kernio.OutputStream,
				Content: chunk.Delta,
			}); err != nil {
				logging.GetLogger().DebugContext(ctx, "stream chunk send failed", "error", err)
			}
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
		if err := l.IO.Send(ctx, kernio.OutputMessage{Type: kernio.OutputStreamEnd}); err != nil {
			logging.GetLogger().DebugContext(ctx, "stream completion send failed", "error", err)
		}
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

func ensureLLMCallError(err error, retryable, fallbackSafe bool, metadata model.LLMCallMetadata) error {
	if err == nil {
		return nil
	}
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		merged := *callErr
		merged.Metadata = mergeLLMMetadata(merged.Metadata, metadata)
		return &merged
	}
	return &model.LLMCallError{
		Err:          err,
		Retryable:    retryable,
		FallbackSafe: fallbackSafe,
		Metadata:     metadata,
	}
}

func mergeLLMMetadata(base, overlay model.LLMCallMetadata) model.LLMCallMetadata {
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
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.Retryable
	}
	return true
}

func llmErrorFallbackSafe(err error) bool {
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.FallbackSafe
	}
	return false
}

func llmMetadataFromResponse(defaultModel string, resp *model.CompletionResponse) model.LLMCallMetadata {
	if resp == nil || resp.Metadata == nil {
		return model.LLMCallMetadata{ActualModel: defaultModel}
	}
	meta := *resp.Metadata
	if strings.TrimSpace(meta.ActualModel) == "" {
		meta.ActualModel = defaultModel
	}
	return meta
}

func llmMetadataFromError(defaultModel string, err error) model.LLMCallMetadata {
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		meta := callErr.Metadata
		if strings.TrimSpace(meta.ActualModel) == "" {
			meta.ActualModel = defaultModel
		}
		return meta
	}
	return model.LLMCallMetadata{ActualModel: defaultModel}
}

func (l *AgentLoop) emitLLMAttemptEvents(ctx context.Context, sessionID string, metadata model.LLMCallMetadata, exhausted bool) {
	for _, attempt := range metadata.Attempts {
		event := l.executionEventBase(&session.Session{ID: sessionID}, observe.ExecutionEventType("llm_failover_attempt"), "llm", "runtime", "llm_attempt")
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
		observe.ObserveExecutionEvent(ctx, l.observer(), event)
		if strings.TrimSpace(attempt.FailoverTo) != "" {
			switchEvent := l.executionEventBase(&session.Session{ID: sessionID}, observe.ExecutionEventType("llm_failover_switch"), "llm", "runtime", "llm_attempt")
			switchEvent.Model = attempt.CandidateModel
			switchEvent.Data = map[string]any{
				"candidate_model": attempt.CandidateModel,
				"failover_to":     attempt.FailoverTo,
				"model_lane":      l.currentTurn.ModelRoute.Lane,
			}
			observe.ObserveExecutionEvent(ctx, l.observer(), switchEvent)
		}
	}
	if exhausted && len(metadata.Attempts) > 0 {
		event := l.executionEventBase(&session.Session{ID: sessionID}, observe.ExecutionEventType("llm_failover_exhausted"), "llm", "runtime", "llm_attempt")
		event.Model = metadata.ActualModel
		observe.ObserveExecutionEvent(ctx, l.observer(), event)
	}
}
