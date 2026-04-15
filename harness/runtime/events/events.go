package events

import (
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"time"
)

// StreamEventType is a frontend-facing normalized runtime event type.
type StreamEventType string

const (
	EventAssistantMessage StreamEventType = "assistant.message"
	EventAssistantDelta   StreamEventType = "assistant.delta"
	EventAssistantDone    StreamEventType = "assistant.done"
	EventProgress         StreamEventType = "runtime.progress"
	EventToolStarted      StreamEventType = "tool.started"
	EventToolCompleted    StreamEventType = "tool.completed"
	EventRunStarted       StreamEventType = "run.started"
	EventRunCompleted     StreamEventType = "run.completed"
	EventRunFailed        StreamEventType = "run.failed"
	EventUnknown          StreamEventType = "unknown"
)

// StreamEvent is a normalized event DTO for UI/transport layers.
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"session_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Metadata  map[string]any  `json:"meta,omitempty"`
}

// FromOutputMessage maps kernel output messages to a normalized stream event.
func FromOutputMessage(msg io.OutputMessage, now time.Time) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: now,
		Content:   msg.Content,
		Metadata:  cloneMap(msg.Meta),
	}
	switch msg.Type {
	case io.OutputText:
		event.Type = EventAssistantMessage
	case io.OutputStream:
		event.Type = EventAssistantDelta
	case io.OutputStreamEnd:
		event.Type = EventAssistantDone
	case io.OutputProgress:
		event.Type = EventProgress
	case io.OutputToolStart:
		event.Type = EventToolStarted
	case io.OutputToolResult:
		event.Type = EventToolCompleted
	}
	return event
}

// FromExecutionEvent maps observer execution events to normalized stream events.
func FromExecutionEvent(e observe.ExecutionEvent) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: e.Timestamp,
		SessionID: e.SessionID,
		Content:   e.Error,
		Metadata:  cloneMap(e.Metadata),
	}
	switch e.Type {
	case observe.ExecutionRunStarted:
		event.Type = EventRunStarted
	case observe.ExecutionRunCompleted:
		event.Type = EventRunCompleted
	case observe.ExecutionRunFailed, observe.ExecutionRunCancelled:
		event.Type = EventRunFailed
	case observe.ExecutionToolStarted:
		event.Type = EventToolStarted
	case observe.ExecutionToolCompleted:
		event.Type = EventToolCompleted
	case observe.ExecutionIterationProgress:
		event.Type = EventProgress
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	if e.ToolName != "" {
		event.Metadata["tool_name"] = e.ToolName
	}
	if e.CallID != "" {
		event.Metadata["call_id"] = e.CallID
	}
	if e.Model != "" {
		event.Metadata["model"] = e.Model
	}
	return event
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
