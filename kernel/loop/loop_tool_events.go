package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/logging"
)

func (l *AgentLoop) emitToolStarted(ctx context.Context, sess *session.Session, call model.ToolCall, spec tool.ToolSpec, repairedArgs json.RawMessage) {
	if l.IO != nil {
		if err := l.IO.Send(ctx, io.OutputMessage{
			Type:    io.OutputToolStart,
			Content: call.Name,
			Meta: map[string]any{
				"call_id":      call.ID,
				"tool":         call.Name,
				"risk":         string(spec.Risk),
				"args_preview": previewToolArguments(repairedArgs),
			},
		}); err != nil {
			logging.GetLogger().DebugContext(ctx, "tool start send failed", "session_id", sess.ID, "tool", call.Name, "error", err)
		}
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), observe.ExecutionEvent{
		Type:         observe.ExecutionToolStarted,
		EventID:      l.nextEventID(string(observe.ExecutionToolStarted)),
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

func (l *AgentLoop) rejectToolCall(
	ctx context.Context,
	sess *session.Session,
	call model.ToolCall,
	spec *tool.ToolSpec,
	repairedArgs json.RawMessage,
	rejectErr error,
) model.ToolResult {
	normalizedErr := normalizeToolError(rejectErr)
	result := buildToolResult(call.ID, nil, rejectErr)
	risk := ""
	if spec != nil {
		risk = string(spec.Risk)
	}
	observe.ObserveToolCall(ctx, l.observer(), observe.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      risk,
		StartedAt: time.Now().UTC(),
		Duration:  0,
		Error:     normalizedErr,
	})
	event := l.executionEventBase(sess, observe.ExecutionToolCompleted, "tool", "runtime", "tool")
	event.ToolName = call.Name
	event.CallID = call.ID
	event.Risk = risk
	event.Metadata = map[string]any{
		"is_error": true,
	}
	event.Error = normalizedErr.Error()
	appendToolErrorMetadata(&event, normalizedErr)
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, spec, nil, result, 0, normalizedErr)
	l.sendToolResultIO(ctx, call, result, 0, normalizedErr)
	return result
}

func (l *AgentLoop) observeToolCompletion(
	ctx context.Context,
	sess *session.Session,
	call model.ToolCall,
	spec tool.ToolSpec,
	toolStart time.Time,
	toolDur time.Duration,
	result model.ToolResult,
	output []byte,
	err error,
) {
	observe.ObserveToolCall(ctx, l.observer(), observe.ToolCallEvent{
		SessionID: sess.ID,
		ToolName:  call.Name,
		Risk:      string(spec.Risk),
		StartedAt: toolStart.UTC(),
		Duration:  toolDur,
		Error:     err,
	})
	event := l.executionEventBase(sess, observe.ExecutionToolCompleted, "tool", "runtime", "tool")
	event.ToolName = call.Name
	event.CallID = call.ID
	event.Risk = string(spec.Risk)
	event.Duration = toolDur
	event.Metadata = map[string]any{
		"is_error": result.IsError,
	}
	if err != nil {
		event.Error = err.Error()
		appendToolErrorMetadata(&event, err)
	}
	appendToolExecutionMetadata(&event, output)
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) emitToolLifecycleAfter(
	ctx context.Context,
	sess *session.Session,
	call model.ToolCall,
	repairedArgs json.RawMessage,
	spec *tool.ToolSpec,
	output []byte,
	result model.ToolResult,
	toolDur time.Duration,
	err error,
) {
	_ = l.emitToolLifecycle(ctx, makeToolEvent(hooks.ToolLifecycleAfter, sess, call, repairedArgs, spec, output, &result, toolDur, err, l.IO, l.observer()))
}

func (l *AgentLoop) sendToolResultIO(ctx context.Context, call model.ToolCall, result model.ToolResult, toolDur time.Duration, err error) {
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
	if sendErr := l.IO.Send(ctx, io.OutputMessage{
		Type:    io.OutputToolResult,
		Content: model.ContentPartsToPlainText(result.ContentParts),
		Meta:    meta,
	}); sendErr != nil {
		logging.GetLogger().DebugContext(ctx, "tool result send failed", "tool", call.Name, "error", sendErr)
	}
}

func (l *AgentLoop) emitToolLifecycle(ctx context.Context, event hooks.ToolEvent) error {
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	var runErr error
	reportErr := func(err error) {
		sessionID := ""
		if event.Session != nil {
			sessionID = event.Session.ID
		}
		slog.Default().ErrorContext(callCtx, "tool lifecycle hook error",
			slog.String("stage", string(event.Stage)),
			slog.String("session_id", sessionID),
			slog.String("tool", toolEventName(event)),
			slog.String("call_id", event.CallID),
			slog.Any("error", err),
		)
		observe.ObserveError(context.Background(), l.observer(), observe.ErrorEvent{
			SessionID: sessionID,
			Phase:     "tool_lifecycle_hook",
			Error:     err,
			Message:   err.Error(),
		})
	}
	// Serialize lifecycle hooks to prevent concurrent mutation of session
	// state (e.g. Config.Metadata writes in session-store persistence hook).
	l.withSideEffectsLock(func() {
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("tool lifecycle hook panic: %v", r)
			}
		}()
		runErr = l.safeHooks().OnToolLifecycle.Run(callCtx, &event)
	})
	if runErr != nil {
		reportErr(runErr)
	}
	return runErr
}

func makeToolEvent(
	stage hooks.ToolStage,
	sess *session.Session,
	call model.ToolCall,
	repairedArgs json.RawMessage,
	spec *tool.ToolSpec,
	output []byte,
	result *model.ToolResult,
	toolDur time.Duration,
	err error,
	userIO io.UserIO,
	observer observe.Observer,
) hooks.ToolEvent {
	risk := ""
	toolName := strings.TrimSpace(call.Name)
	if spec != nil {
		risk = string(spec.Risk)
		if strings.TrimSpace(spec.Name) != "" {
			toolName = spec.Name
		}
	}
	return hooks.ToolEvent{
		Stage:      stage,
		Session:    sess,
		Tool:       spec,
		ToolName:   toolName,
		CallID:     strings.TrimSpace(call.ID),
		Input:      append(json.RawMessage(nil), repairedArgs...),
		Output:     append(json.RawMessage(nil), output...),
		ToolResult: result,
		Risk:       risk,
		Duration:   toolDur,
		Error:      err,
		Timestamp:  time.Now().UTC(),
		IO:         userIO,
		Observer:   observer,
	}
}

func toolEventName(event hooks.ToolEvent) string {
	if event.Tool != nil && strings.TrimSpace(event.Tool.Name) != "" {
		return event.Tool.Name
	}
	return strings.TrimSpace(event.ToolName)
}

func appendToolExecutionMetadata(event *observe.ExecutionEvent, output json.RawMessage) {
	if event == nil || len(output) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		return
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	for _, key := range []string{"enforcement", "degraded", "details", "url", "method", "status_code", "follow_redirects"} {
		if value, ok := payload[key]; ok {
			event.Metadata[key] = value
		}
	}
}

func appendExecutionErrorMetadata(event *observe.ExecutionEvent, err error) {
	if event == nil || err == nil {
		return
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	code := string(kerrors.GetCode(err))
	if code != "" {
		event.Metadata["error_code"] = code
	}
	var kernelErr *kerrors.Error
	if errors.As(err, &kernelErr) && len(kernelErr.Meta) > 0 {
		for k, v := range kernelErr.Meta {
			event.Metadata[k] = v
		}
	}
}

func appendToolErrorMetadata(event *observe.ExecutionEvent, err error) {
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
