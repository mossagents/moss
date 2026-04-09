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
	intr "github.com/mossagents/moss/kernel/io"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session, plan TurnPlan) (*mdl.CompletionResponse, bool, error) {
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
	req := mdl.CompletionRequest{
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

func (l *AgentLoop) callLLMOnce(ctx context.Context, req mdl.CompletionRequest) callAttemptResult {
	// 熔断器检查
	if b := l.Config.LLMBreaker; b != nil {
		if !b.Allow() {
			return callAttemptResult{
				err: &mdl.LLMCallError{
					Err:       kerrors.New(kerrors.ErrLLMRejected, "LLM circuit breaker is open: too many recent failures"),
					Retryable: false,
				},
				retryable: false,
			}
		}
	}

	// 优先使用 Streaming
	if sllm, ok := l.LLM.(mdl.StreamingLLM); ok {
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

func (l *AgentLoop) streamLLM(ctx context.Context, sllm mdl.StreamingLLM, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	iter, err := sllm.Stream(ctx, req)
	if err != nil {
		return nil, ensureLLMCallError(err, true, true, mdl.LLMCallMetadata{})
	}
	defer func() { _ = iter.Close() }()
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

	msg := mdl.Message{
		Role:      mdl.RoleAssistant,
		ToolCalls: state.toolCalls,
	}
	if state.fullReasoning != "" {
		msg.ContentParts = append(msg.ContentParts, mdl.ReasoningPart(state.fullReasoning))
	}
	if state.fullContent != "" {
		msg.ContentParts = append(msg.ContentParts, mdl.TextPart(state.fullContent))
	}

	return &mdl.CompletionResponse{
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
	toolCalls      []mdl.ToolCall
	usage          mdl.TokenUsage
	stopReason     string
	emittedContent bool
}

func metadataStreamProvider(iter mdl.StreamIterator) mdl.MetadataStreamIterator {
	if provider, ok := iter.(mdl.MetadataStreamIterator); ok {
		return provider
	}
	return nil
}

func (l *AgentLoop) handleStreamChunkError(
	ctx context.Context,
	err error,
	metadataProvider mdl.MetadataStreamIterator,
	state *streamAccumulator,
) (bool, error) {
	if state.emittedContent && len(state.toolCalls) > 0 && isRecoverableStreamTailError(err) {
		state.stopReason = "tool_use"
		if l.IO != nil {
			if sendErr := l.IO.Send(ctx, intr.OutputMessage{Type: intr.OutputStreamEnd}); sendErr != nil {
				logging.GetLogger().DebugContext(ctx, "stream end send failed", "error", sendErr)
			}
		}
		return true, nil
	}
	safePreEmission := !state.emittedContent && len(state.toolCalls) == 0
	return false, ensureLLMCallError(err, safePreEmission, safePreEmission, streamMetadata(metadataProvider))
}

func (l *AgentLoop) applyStreamChunk(ctx context.Context, chunk mdl.StreamChunk, state *streamAccumulator) bool {
	if chunk.ReasoningDelta != "" {
		state.emittedContent = true
		state.fullReasoning += chunk.ReasoningDelta
		if l.IO != nil {
			if err := l.IO.Send(ctx, intr.OutputMessage{
				Type:    intr.OutputReasoning,
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
			if err := l.IO.Send(ctx, intr.OutputMessage{
				Type:    intr.OutputStream,
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
		if err := l.IO.Send(ctx, intr.OutputMessage{Type: intr.OutputStreamEnd}); err != nil {
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

func streamMetadata(provider mdl.MetadataStreamIterator) mdl.LLMCallMetadata {
	if provider == nil {
		return mdl.LLMCallMetadata{}
	}
	return provider.Metadata()
}

func metadataPtr(meta mdl.LLMCallMetadata) *mdl.LLMCallMetadata {
	if strings.TrimSpace(meta.ActualModel) == "" && len(meta.Attempts) == 0 {
		return nil
	}
	copyMeta := meta
	return &copyMeta
}

func ensureLLMCallError(err error, retryable, fallbackSafe bool, metadata mdl.LLMCallMetadata) error {
	if err == nil {
		return nil
	}
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		merged := *callErr
		merged.Metadata = mergeLLMMetadata(merged.Metadata, metadata)
		return &merged
	}
	return &mdl.LLMCallError{
		Err:          err,
		Retryable:    retryable,
		FallbackSafe: fallbackSafe,
		Metadata:     metadata,
	}
}

func mergeLLMMetadata(base, overlay mdl.LLMCallMetadata) mdl.LLMCallMetadata {
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
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.Retryable
	}
	return true
}

func llmErrorFallbackSafe(err error) bool {
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.FallbackSafe
	}
	return false
}

func llmMetadataFromResponse(defaultModel string, resp *mdl.CompletionResponse) mdl.LLMCallMetadata {
	if resp == nil || resp.Metadata == nil {
		return mdl.LLMCallMetadata{ActualModel: defaultModel}
	}
	meta := *resp.Metadata
	if strings.TrimSpace(meta.ActualModel) == "" {
		meta.ActualModel = defaultModel
	}
	return meta
}

func llmMetadataFromError(defaultModel string, err error) mdl.LLMCallMetadata {
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		meta := callErr.Metadata
		if strings.TrimSpace(meta.ActualModel) == "" {
			meta.ActualModel = defaultModel
		}
		return meta
	}
	return mdl.LLMCallMetadata{ActualModel: defaultModel}
}

func (l *AgentLoop) emitLLMAttemptEvents(ctx context.Context, sessionID string, metadata mdl.LLMCallMetadata, exhausted bool) {
	for _, attempt := range metadata.Attempts {
		event := l.executionEventBase(&session.Session{ID: sessionID}, kobs.ExecutionEventType("llm_failover_attempt"), "llm", "runtime", "llm_attempt")
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
		kobs.ObserveExecutionEvent(ctx, l.observer(), event)
		if strings.TrimSpace(attempt.FailoverTo) != "" {
			switchEvent := l.executionEventBase(&session.Session{ID: sessionID}, kobs.ExecutionEventType("llm_failover_switch"), "llm", "runtime", "llm_attempt")
			switchEvent.Model = attempt.CandidateModel
			switchEvent.Data = map[string]any{
				"candidate_model": attempt.CandidateModel,
				"failover_to":     attempt.FailoverTo,
				"model_lane":      l.currentTurn.ModelRoute.Lane,
			}
			kobs.ObserveExecutionEvent(ctx, l.observer(), switchEvent)
		}
	}
	if exhausted && len(metadata.Attempts) > 0 {
		event := l.executionEventBase(&session.Session{ID: sessionID}, kobs.ExecutionEventType("llm_failover_exhausted"), "llm", "runtime", "llm_attempt")
		event.Model = metadata.ActualModel
		kobs.ObserveExecutionEvent(ctx, l.observer(), event)
	}
}
