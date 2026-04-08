package events

import (
	intr "github.com/mossagents/moss/kernel/io"
	kobs "github.com/mossagents/moss/kernel/observe"
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
	Meta      map[string]any  `json:"meta,omitempty"`
}

// FromOutputMessage maps kernel output messages to a normalized stream event.
func FromOutputMessage(msg intr.OutputMessage, now time.Time) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: now,
		Content:   msg.Content,
		Meta:      cloneMap(msg.Meta),
	}
	switch msg.Type {
	case intr.OutputText:
		event.Type = EventAssistantMessage
	case intr.OutputStream:
		event.Type = EventAssistantDelta
	case intr.OutputStreamEnd:
		event.Type = EventAssistantDone
	case intr.OutputProgress:
		event.Type = EventProgress
	case intr.OutputToolStart:
		event.Type = EventToolStarted
	case intr.OutputToolResult:
		event.Type = EventToolCompleted
	}
	return event
}

// FromExecutionEvent maps observer execution events to normalized stream events.
func FromExecutionEvent(e kobs.ExecutionEvent) StreamEvent {
	event := StreamEvent{
		Type:      EventUnknown,
		Timestamp: e.Timestamp,
		SessionID: e.SessionID,
		Content:   e.Error,
		Meta:      cloneMap(e.Data),
	}
	switch e.Type {
	case kobs.ExecutionRunStarted:
		event.Type = EventRunStarted
	case kobs.ExecutionRunCompleted:
		event.Type = EventRunCompleted
	case kobs.ExecutionRunFailed, kobs.ExecutionRunCancelled:
		event.Type = EventRunFailed
	case kobs.ExecutionToolStarted:
		event.Type = EventToolStarted
	case kobs.ExecutionToolCompleted:
		event.Type = EventToolCompleted
	case kobs.ExecutionIterationProgress:
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
