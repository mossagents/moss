package observe

import (
	"context"
	"log/slog"
)

// LogObserver emits structured log entries (slog) for all observer events.
// Useful for development, debugging, and log-based monitoring.
type LogObserver struct {
	NoOpObserver
	logger *slog.Logger
	level  slog.Level
}

// NewLogObserver creates a LogObserver that emits slog records at the given level.
func NewLogObserver(logger *slog.Logger, level slog.Level) *LogObserver {
	return &LogObserver{
		logger: logger,
		level:  level,
	}
}

// OnLLMCall emits a structured log for an LLM call completion event.
func (o *LogObserver) OnLLMCall(ctx context.Context, e LLMCallEvent) {
	attrs := []slog.Attr{
		slog.String("session_id", e.SessionID),
		slog.String("model", e.Model),
		slog.Duration("duration", e.Duration),
		slog.Int("prompt_tokens", e.Usage.PromptTokens),
		slog.Int("completion_tokens", e.Usage.CompletionTokens),
		slog.String("stop_reason", e.StopReason),
		slog.Bool("streamed", e.Streamed),
	}
	if e.Error != nil {
		attrs = append(attrs, slog.String("error", e.Error.Error()))
	}
	o.log(ctx, "llm.call", attrs)
}

// OnToolCall emits a structured log for a tool call completion event.
func (o *LogObserver) OnToolCall(ctx context.Context, e ToolCallEvent) {
	attrs := []slog.Attr{
		slog.String("session_id", e.SessionID),
		slog.String("tool_name", e.ToolName),
		slog.String("risk", e.Risk),
		slog.Duration("duration", e.Duration),
	}
	if e.Error != nil {
		attrs = append(attrs, slog.String("error", e.Error.Error()))
	}
	o.log(ctx, "tool.call", attrs)
}

// OnSessionEvent emits a structured log for session lifecycle events.
func (o *LogObserver) OnSessionEvent(ctx context.Context, e SessionEvent) {
	o.log(ctx, "session.event", []slog.Attr{
		slog.String("session_id", e.SessionID),
		slog.String("type", e.Type),
	})
}

// OnError emits a structured log for unexpected error events.
func (o *LogObserver) OnError(ctx context.Context, e ErrorEvent) {
	attrs := []slog.Attr{
		slog.String("session_id", e.SessionID),
		slog.String("phase", e.Phase),
		slog.String("message", e.Message),
	}
	if e.Error != nil {
		attrs = append(attrs, slog.String("error", e.Error.Error()))
	}
	o.log(ctx, "error", attrs)
}

func (o *LogObserver) log(ctx context.Context, msg string, attrs []slog.Attr) {
	if !o.logger.Enabled(ctx, o.level) {
		return
	}
	o.logger.LogAttrs(ctx, o.level, msg, attrs...)
}

// ensure LogObserver satisfies Observer at compile time
var _ Observer = (*LogObserver)(nil)
