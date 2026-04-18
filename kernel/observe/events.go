package observe

import (
	"github.com/mossagents/moss/kernel/io"
	taskrt "github.com/mossagents/moss/kernel/task"
	"time"
)

// EventKind 表示统一运行时事件的分类。
type EventKind string

const (
	EventKindLLM       EventKind = "llm"
	EventKindTool      EventKind = "tool"
	EventKindExecution EventKind = "execution"
	EventKindApproval  EventKind = "approval"
	EventKindSession   EventKind = "session"
	EventKindError     EventKind = "error"
	EventKindTurn      EventKind = "turn"
	EventKindTask      EventKind = "task"
)

// TurnEventType 表示 turn 级别事件。
type TurnEventType string

const (
	TurnEventStarted   TurnEventType = "started"
	TurnEventProgress  TurnEventType = "progress"
	TurnEventCompleted TurnEventType = "completed"
	TurnEventFailed    TurnEventType = "failed"
	TurnEventCancelled TurnEventType = "cancelled"
)

// TurnEvent 是对 ExecutionEvent 的 turn 级抽象。
type TurnEvent struct {
	Type      TurnEventType  `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	TurnID    string         `json:"turn_id,omitempty"`
	ParentID  string         `json:"parent_id,omitempty"`
	Model     string         `json:"model,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
	Error     string         `json:"error,omitempty"`
	Metadata  map[string]any `json:"data,omitempty"`
}

// TaskEventType 表示任务图事件。
type TaskEventType string

const (
	TaskEventUpserted TaskEventType = "upserted"
	TaskEventClaimed  TaskEventType = "claimed"
	TaskEventUpdated  TaskEventType = "updated"
)

// TaskEvent 描述任务/子代理关系相关事件。
type TaskEvent struct {
	Type      TaskEventType         `json:"type"`
	Timestamp time.Time             `json:"timestamp,omitempty"`
	Task      taskrt.TaskSummary    `json:"task"`
	Relations []taskrt.TaskRelation `json:"relations,omitempty"`
}

// EventEnvelope 是统一运行时事件封装。
type EventEnvelope struct {
	Kind      EventKind         `json:"kind"`
	SessionID string            `json:"session_id,omitempty"`
	RunID     string            `json:"run_id,omitempty"`
	TurnID    string            `json:"turn_id,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
	LLM       *LLMCallEvent     `json:"llm,omitempty"`
	Tool      *ToolCallEvent    `json:"tool,omitempty"`
	Execution *ExecutionEvent   `json:"execution,omitempty"`
	Approval  *io.ApprovalEvent `json:"approval,omitempty"`
	Session   *SessionEvent     `json:"session,omitempty"`
	Error     *ErrorEvent       `json:"error,omitempty"`
	Turn      *TurnEvent        `json:"turn,omitempty"`
	Task      *TaskEvent        `json:"task,omitempty"`
}

func EnvelopeFromLLMCall(e LLMCallEvent) EventEnvelope {
	return EventEnvelope{
		Kind:      EventKindLLM,
		SessionID: e.SessionID,
		Timestamp: eventTimeOrNow(e.StartedAt),
		LLM:       &e,
	}
}

func EnvelopeFromToolCall(e ToolCallEvent) EventEnvelope {
	return EventEnvelope{
		Kind:      EventKindTool,
		SessionID: e.SessionID,
		Timestamp: eventTimeOrNow(e.StartedAt),
		Tool:      &e,
	}
}

func EnvelopeFromExecutionEvent(e ExecutionEvent) EventEnvelope {
	envelope := EventEnvelope{
		Kind:      EventKindExecution,
		SessionID: e.SessionID,
		RunID:     e.RunID,
		TurnID:    e.TurnID,
		ParentID:  e.ParentID,
		Timestamp: eventTimeOrNow(e.Timestamp),
		Execution: &e,
	}
	if turn := TurnEventFromExecutionEvent(e); turn != nil {
		envelope.Turn = turn
		envelope.Kind = EventKindTurn
	}
	return envelope
}

func EnvelopeFromApprovalEvent(e io.ApprovalEvent) EventEnvelope {
	timestamp := e.Request.RequestedAt
	if e.Decision != nil && !e.Decision.DecidedAt.IsZero() {
		timestamp = e.Decision.DecidedAt
	}
	return EventEnvelope{
		Kind:      EventKindApproval,
		SessionID: e.SessionID,
		Timestamp: eventTimeOrNow(timestamp),
		Approval:  &e,
	}
}

func EnvelopeFromSessionEvent(e SessionEvent) EventEnvelope {
	return EventEnvelope{
		Kind:      EventKindSession,
		SessionID: e.SessionID,
		Timestamp: time.Now().UTC(),
		Session:   &e,
	}
}

func EnvelopeFromErrorEvent(e ErrorEvent) EventEnvelope {
	return EventEnvelope{
		Kind:      EventKindError,
		SessionID: e.SessionID,
		Timestamp: time.Now().UTC(),
		Error:     &e,
	}
}

func EnvelopeFromTaskEvent(e TaskEvent) EventEnvelope {
	return EventEnvelope{
		Kind:      EventKindTask,
		SessionID: e.Task.Handle.SessionID,
		Timestamp: eventTimeOrNow(e.Timestamp),
		Task:      &e,
	}
}

func TurnEventFromExecutionEvent(e ExecutionEvent) *TurnEvent {
	if e.TurnID == "" && e.Type != ExecutionRunStarted && e.Type != ExecutionRunCompleted && e.Type != ExecutionRunFailed && e.Type != ExecutionRunCancelled {
		return nil
	}
	typ := mapTurnEventType(e.Type)
	if typ == "" {
		return nil
	}
	return &TurnEvent{
		Type:      typ,
		SessionID: e.SessionID,
		RunID:     e.RunID,
		TurnID:    e.TurnID,
		ParentID:  e.ParentID,
		Model:     e.Model,
		Timestamp: eventTimeOrNow(e.Timestamp),
		Error:     e.Error,
		Metadata:  cloneEventMetadata(e.Metadata),
	}
}

func mapTurnEventType(typ ExecutionEventType) TurnEventType {
	switch typ {
	case ExecutionRunStarted, ExecutionIterationStarted:
		return TurnEventStarted
	case ExecutionIterationProgress, ExecutionLLMStarted, ExecutionLLMCompleted, ExecutionToolStarted, ExecutionToolCompleted,
		ExecutionHostedToolStarted, ExecutionHostedToolProgress, ExecutionHostedToolCompleted, ExecutionHostedToolFailed:
		return TurnEventProgress
	case ExecutionRunCompleted:
		return TurnEventCompleted
	case ExecutionRunFailed:
		return TurnEventFailed
	case ExecutionRunCancelled:
		return TurnEventCancelled
	default:
		return ""
	}
}

func cloneEventMetadata(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = value
	}
	return out
}

func eventTimeOrNow(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}
