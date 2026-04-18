package loop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session, plan TurnPlan) (*model.CompletionResponse, error) {
	specs := l.toolSpecs(plan)
	promptMessages, normalizeStats := session.PromptMessagesWithStats(sess)
	if normalizeStats.Changed() {
		l.emitPromptNormalizationEvent(ctx, sess, normalizeStats)
	}
	l.logger().DebugContext(ctx, "llm request prepared",
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

	// Context-window trim retry: 当 provider 返回上下文超限错误时，
	// 采用 FIFO 修剪最老的非 system 消息并重试，避免直接失败。
	const maxTrimRetries = 5
	for trimAttempt := 0; trimAttempt <= maxTrimRetries; trimAttempt++ {
		resp, err := l.callLLMWithRetry(ctx, sess, req)
		if err == nil {
			return resp, nil
		}
		if !isContextWindowExceededError(err) || trimAttempt == maxTrimRetries {
			return nil, err
		}
		trimmed, ok := trimOldestPromptMessage(req.Messages)
		if !ok {
			return nil, err
		}
		l.emitContextTrimRetryEvent(ctx, sess, trimAttempt+1, req.Messages, trimmed)
		l.logger().WarnContext(ctx, "context window exceeded; trimming oldest prompt message and retrying",
			"session_id", sess.ID,
			"turn_id", plan.TurnID,
			"trim_attempt", trimAttempt+1,
			"messages_before", len(req.Messages),
			"messages_after", len(trimmed),
		)
		req.Messages = trimmed
	}

	return nil, &model.LLMCallError{Err: errors.New("context trim retry exhausted"), Retryable: false}
}

func (l *AgentLoop) emitPromptNormalizationEvent(ctx context.Context, sess *session.Session, stats session.PromptNormalizationStats) {
	if !stats.Changed() {
		return
	}
	event := l.executionEventBase(sess, observe.ExecutionContextNormalized, "prompt", "runtime", "prompt_normalization")
	event.Metadata = map[string]any{
		"input_messages":                   stats.InputMessages,
		"output_messages":                  stats.OutputMessages,
		"dropped_orphan_tool_results":      stats.DroppedOrphanToolResults,
		"synthesized_missing_tool_results": stats.SynthesizedMissingToolResults,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) emitContextTrimRetryEvent(ctx context.Context, sess *session.Session, attempt int, before, after []model.Message) {
	event := l.executionEventBase(sess, observe.ExecutionContextTrimRetry, "llm", "runtime", "prompt_trim")
	event.Metadata = map[string]any{
		"trim_attempt":            attempt,
		"messages_before":         len(before),
		"messages_after":          len(after),
		"messages_removed":        len(before) - len(after),
		"estimated_tokens_before": session.EstimateMessagesTokens(before),
		"estimated_tokens_after":  session.EstimateMessagesTokens(after),
		"trigger":                 "context_window",
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) callLLMWithRetry(ctx context.Context, sess *session.Session, req model.CompletionRequest) (*model.CompletionResponse, error) {
	cfg := l.Config.LLMRetry
	if !cfg.Enabled() {
		attempt := l.callLLMOnce(ctx, sess, req)
		return attempt.resp, attempt.err
	}

	maxRetries := cfg.MaxRetriesOrDefault()
	delay := cfg.InitialDelayOrDefault()
	var lastErr error

	for attemptIndex := 0; attemptIndex <= maxRetries; attemptIndex++ {
		attempt := l.callLLMOnce(ctx, sess, req)
		if attempt.err == nil {
			return attempt.resp, nil
		}

		lastErr = attempt.err
		if !attempt.retryable || !cfg.ShouldRetryOrDefault(ctx, attempt.err) || attemptIndex == maxRetries {
			return nil, attempt.err
		}

		jitter := time.Duration(rand.Int64N(int64(delay) / 2))
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

func (l *AgentLoop) callLLMOnce(ctx context.Context, sess *session.Session, req model.CompletionRequest) callAttemptResult {
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
				l.finalizeHostedToolLifecycle(ctx, sess, &state, false)
				if l.IO != nil {
					if sendErr := l.IO.Send(ctx, kernio.OutputMessage{Type: kernio.OutputStreamEnd}); sendErr != nil {
						l.logger().DebugContext(ctx, "stream end send failed", "error", sendErr)
					}
				}
				break
			}
			l.finalizeHostedToolLifecycle(ctx, sess, &state, true)
			safePreEmission := !state.emittedContent && len(state.toolCalls) == 0
			llmErr := ensureLLMCallError(err, safePreEmission, safePreEmission, derefMetadata(metadata))
			l.recordBreakerFailure()
			return callAttemptResult{retryable: llmErrorRetryable(llmErr), err: llmErr}
		}

		// Extract metadata from chunks (typically on the Done chunk).
		if chunk.Metadata != nil {
			metadata = chunk.Metadata
		}
		if done := l.applyStreamChunk(ctx, sess, chunk, &state); done {
			break
		}
	}

	l.recordBreakerSuccess()

	msg := model.Message{
		Role:            model.RoleAssistant,
		ToolCalls:       state.toolCalls,
		HostedToolCalls: state.hostedToolCalls,
	}
	if state.fullReasoning != "" {
		msg.ContentParts = append(msg.ContentParts, model.ReasoningPart(state.fullReasoning))
	}
	if state.fullRefusal != "" {
		msg.ContentParts = append(msg.ContentParts, model.RefusalPart(state.fullRefusal))
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
	fullContent      string
	fullReasoning    string
	fullRefusal      string
	toolCalls        []model.ToolCall
	hostedToolCalls  []model.HostedToolEvent
	hostedToolPhases map[string]string
	usage            model.TokenUsage
	stopReason       string
	emittedContent   bool
}

func derefMetadata(m *model.LLMCallMetadata) model.LLMCallMetadata {
	if m == nil {
		return model.LLMCallMetadata{}
	}
	return *m
}

func (l *AgentLoop) applyStreamChunk(ctx context.Context, sess *session.Session, chunk model.StreamChunk, state *streamAccumulator) bool {
	chunk = chunk.Normalized()
	switch chunk.Type {
	case model.StreamChunkReasoningDelta:
		state.emittedContent = true
		state.fullReasoning += chunk.Content
		if l.IO != nil {
			if err := l.IO.Send(ctx, kernio.OutputMessage{
				Type:    kernio.OutputReasoning,
				Content: chunk.Content,
			}); err != nil {
				l.logger().DebugContext(ctx, "reasoning send failed", "error", err)
			}
		}
	case model.StreamChunkRefusalDelta:
		state.emittedContent = true
		state.fullRefusal += chunk.Content
		if l.IO != nil {
			if err := l.IO.Send(ctx, kernio.OutputMessage{
				Type:    kernio.OutputRefusal,
				Content: chunk.Content,
			}); err != nil {
				l.logger().DebugContext(ctx, "refusal send failed", "error", err)
			}
		}
	case model.StreamChunkTextDelta:
		state.emittedContent = true
		state.fullContent += chunk.Content
		if l.IO != nil {
			if err := l.IO.Send(ctx, kernio.OutputMessage{
				Type:    kernio.OutputStream,
				Content: chunk.Content,
			}); err != nil {
				l.logger().DebugContext(ctx, "stream chunk send failed", "error", err)
			}
		}
	case model.StreamChunkToolCall:
		state.emittedContent = true
		if chunk.ToolCall != nil {
			state.toolCalls = append(state.toolCalls, *chunk.ToolCall)
		}
	case model.StreamChunkHostedTool:
		state.emittedContent = true
		if chunk.HostedTool != nil {
			state.hostedToolCalls = appendOrReplaceHostedTool(state.hostedToolCalls, *chunk.HostedTool)
			l.observeHostedToolLifecycle(ctx, sess, *chunk.HostedTool, state)
			if l.IO != nil {
				if err := l.IO.Send(ctx, kernio.OutputMessage{
					Type:    kernio.OutputHostedTool,
					Content: describeHostedTool(*chunk.HostedTool),
					Meta:    hostedToolMeta(*chunk.HostedTool),
				}); err != nil {
					l.logger().DebugContext(ctx, "hosted tool send failed", "error", err)
				}
			}
		}
	}

	if !chunk.IsDone() {
		return false
	}
	if chunk.Usage != nil {
		state.usage = *chunk.Usage
	}
	state.stopReason = strings.TrimSpace(chunk.StopReason)
	if state.stopReason == "" {
		state.stopReason = "end_turn"
		if len(state.toolCalls) > 0 {
			state.stopReason = "tool_use"
		}
	}
	l.finalizeHostedToolLifecycle(ctx, sess, state, state.stopReason == "incomplete")
	if l.IO != nil {
		if err := l.IO.Send(ctx, kernio.OutputMessage{Type: kernio.OutputStreamEnd}); err != nil {
			l.logger().DebugContext(ctx, "stream completion send failed", "error", err)
		}
	}
	return true
}

func appendOrReplaceHostedTool(existing []model.HostedToolEvent, event model.HostedToolEvent) []model.HostedToolEvent {
	key := strings.TrimSpace(event.ID)
	if key == "" {
		key = strings.TrimSpace(event.Name)
	}
	for i := range existing {
		candidate := strings.TrimSpace(existing[i].ID)
		if candidate == "" {
			candidate = strings.TrimSpace(existing[i].Name)
		}
		if candidate == key && key != "" {
			existing[i] = event
			return existing
		}
	}
	return append(existing, event)
}

func describeHostedTool(event model.HostedToolEvent) string {
	name := strings.TrimSpace(event.Name)
	status := strings.TrimSpace(event.Status)
	switch {
	case name != "" && status != "":
		return name + " " + status
	case name != "":
		return name
	case status != "":
		return status
	default:
		return "hosted tool"
	}
}

func hostedToolMeta(event model.HostedToolEvent) map[string]any {
	meta := map[string]any{}
	if strings.TrimSpace(event.ID) != "" {
		meta["id"] = event.ID
	}
	if strings.TrimSpace(event.Name) != "" {
		meta["name"] = event.Name
	}
	if strings.TrimSpace(event.Status) != "" {
		meta["status"] = event.Status
	}
	if len(event.Input) > 0 {
		meta["input"] = string(event.Input)
	}
	if len(event.Output) > 0 {
		meta["output"] = string(event.Output)
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func (l *AgentLoop) observeHostedToolLifecycle(ctx context.Context, sess *session.Session, event model.HostedToolEvent, state *streamAccumulator) {
	if state == nil {
		return
	}
	key := hostedToolKey(event)
	if key == "" {
		return
	}
	if state.hostedToolPhases == nil {
		state.hostedToolPhases = map[string]string{}
	}
	current := state.hostedToolPhases[key]
	if current == "" {
		l.emitHostedToolExecutionEvent(ctx, sess, observe.ExecutionHostedToolStarted, event, false)
		state.hostedToolPhases[key] = "started"
		current = "started"
	}
	next := normalizeHostedToolPhase(event.Status)
	if next == "" || next == current {
		return
	}
	l.emitHostedToolExecutionEvent(ctx, sess, hostedToolExecutionEventType(next), event, false)
	state.hostedToolPhases[key] = next
}

func (l *AgentLoop) finalizeHostedToolLifecycle(ctx context.Context, sess *session.Session, state *streamAccumulator, failed bool) {
	if state == nil || len(state.hostedToolCalls) == 0 {
		return
	}
	if state.hostedToolPhases == nil {
		state.hostedToolPhases = map[string]string{}
	}
	terminalPhase := "completed"
	terminalType := observe.ExecutionHostedToolCompleted
	if failed {
		terminalPhase = "failed"
		terminalType = observe.ExecutionHostedToolFailed
	}
	for i := range state.hostedToolCalls {
		event := state.hostedToolCalls[i]
		key := hostedToolKey(event)
		if key == "" {
			continue
		}
		if isHostedToolTerminalPhase(state.hostedToolPhases[key]) {
			continue
		}
		event.Status = terminalPhase
		l.emitHostedToolExecutionEvent(ctx, sess, terminalType, event, true)
		state.hostedToolPhases[key] = terminalPhase
	}
}

func (l *AgentLoop) emitHostedToolExecutionEvent(ctx context.Context, sess *session.Session, typ observe.ExecutionEventType, event model.HostedToolEvent, synthetic bool) {
	exec := l.executionEventBase(sess, typ, "llm", "provider", "hosted_tool")
	exec.ToolName = strings.TrimSpace(event.Name)
	exec.CallID = hostedToolKey(event)
	exec.Metadata = hostedToolMeta(event)
	if exec.Metadata == nil {
		exec.Metadata = map[string]any{}
	}
	exec.Metadata["synthetic_terminal"] = synthetic
	exec.Metadata["read_only"] = isReadOnlyHostedTool(exec.ToolName)
	observe.ObserveExecutionEvent(ctx, l.observer(), exec)
}

func hostedToolKey(event model.HostedToolEvent) string {
	if id := strings.TrimSpace(event.ID); id != "" {
		return id
	}
	return strings.TrimSpace(event.Name)
}

func normalizeHostedToolPhase(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "queued", "pending":
		return ""
	case "searching", "in_progress", "in-progress", "processing", "running", "interpreting":
		return "progress"
	case "completed", "complete", "done", "succeeded", "success":
		return "completed"
	case "failed", "error", "errored", "cancelled", "canceled", "incomplete":
		return "failed"
	default:
		return "progress"
	}
}

func hostedToolExecutionEventType(phase string) observe.ExecutionEventType {
	switch strings.TrimSpace(phase) {
	case "progress":
		return observe.ExecutionHostedToolProgress
	case "failed":
		return observe.ExecutionHostedToolFailed
	case "completed":
		return observe.ExecutionHostedToolCompleted
	default:
		return observe.ExecutionHostedToolProgress
	}
}

func isHostedToolTerminalPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case "completed", "failed":
		return true
	default:
		return false
	}
}

func isReadOnlyHostedTool(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(name, "file_search") || strings.Contains(name, "web_search") || strings.Contains(name, "image_generation")
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
		if merged.Class == "" {
			merged.Class = model.ClassifyError(merged.Err)
		}
		merged.Metadata = mergeLLMMetadata(merged.Metadata, metadata)
		return &merged
	}
	return &model.LLMCallError{
		Err:          err,
		Class:        model.ClassifyError(err),
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
		event.Metadata = map[string]any{
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
			switchEvent.Metadata = map[string]any{
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

func isContextWindowExceededError(err error) bool {
	if err == nil {
		return false
	}
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) && callErr != nil && callErr.Class == model.LLMErrorContextWindow {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context window") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "input token")
}

// trimOldestPromptMessage FIFO 移除最老的非 system 消息。
// 为避免破坏 tool call / tool result 配对：
// - assistant(tool call) + 后续 user(tool result) 会被一起删除
// - orphan tool result 也会被清理掉
func trimOldestPromptMessage(messages []model.Message) ([]model.Message, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	idx := -1
	for i, msg := range messages {
		if msg.Role != model.RoleSystem {
			idx = i
			break
		}
	}
	if idx < 0 {
		return messages, false
	}

	end := idx + 1
	if len(messages[idx].ToolCalls) > 0 && end < len(messages) {
		// If this assistant message initiated tool calls, also remove immediate tool result carrier(s).
		for end < len(messages) {
			next := messages[end]
			if next.Role == model.RoleUser && len(next.ToolResults) > 0 {
				end++
				continue
			}
			break
		}
	}

	trimmed := append([]model.Message(nil), messages[:idx]...)
	trimmed = append(trimmed, messages[end:]...)
	return trimmed, true
}
