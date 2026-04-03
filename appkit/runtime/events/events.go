package events

import (
	"time"

	"github.com/mossagents/moss/kernel/port"
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
	Meta      map[string]any  `json:"meta,omitempty"`
}

// FromOutputMessage maps kernel output messages to a normalized stream event.
func FromOutputMessage(msg port.OutputMessage, now time.Time) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: now,
		Content:   msg.Content,
		Meta:      cloneMap(msg.Meta),
	}
	switch msg.Type {
	case port.OutputText:
		event.Type = EventAssistantMessage
	case port.OutputStream:
		event.Type = EventAssistantDelta
	case port.OutputStreamEnd:
		event.Type = EventAssistantDone
	case port.OutputProgress:
		event.Type = EventProgress
	case port.OutputToolStart:
		event.Type = EventToolStarted
	case port.OutputToolResult:
		event.Type = EventToolCompleted
	}
	return event
}

// FromExecutionEvent maps observer execution events to normalized stream events.
func FromExecutionEvent(e port.ExecutionEvent) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: e.Timestamp,
		SessionID: e.SessionID,
		Content:   e.Error,
		Meta:      cloneMap(e.Data),
	}
	switch e.Type {
	case port.ExecutionRunStarted:
		event.Type = EventRunStarted
	case port.ExecutionRunCompleted:
		event.Type = EventRunCompleted
	case port.ExecutionRunFailed, port.ExecutionRunCancelled:
		event.Type = EventRunFailed
	case port.ExecutionToolStarted:
		event.Type = EventToolStarted
	case port.ExecutionToolCompleted:
		event.Type = EventToolCompleted
	case port.ExecutionIterationProgress:
		event.Type = EventProgress
	}
	if event.Meta == nil {
		event.Meta = map[string]any{}
	}
	if e.ToolName != "" {
		event.Meta["tool_name"] = e.ToolName
	}
	if e.CallID != "" {
		event.Meta["call_id"] = e.CallID
	}
	if e.Model != "" {
		event.Meta["model"] = e.Model
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
