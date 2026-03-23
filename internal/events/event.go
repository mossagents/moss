package events

import "time"

type EventType string

const (
	EventRunStarted          EventType = "run.started"
	EventRunCompleted        EventType = "run.completed"
	EventRunFailed           EventType = "run.failed"
	EventTaskDelegated       EventType = "task.delegated"
	EventTaskCompleted       EventType = "task.completed"
	EventTaskFailed          EventType = "task.failed"
	EventToolStarted         EventType = "tool.started"
	EventToolCompleted       EventType = "tool.completed"
	EventToolFailed          EventType = "tool.failed"
	EventApprovalRequested   EventType = "approval.requested"
	EventApprovalApproved    EventType = "approval.approved"
	EventApprovalRejected    EventType = "approval.rejected"
	EventValidationStarted   EventType = "validation.started"
	EventValidationCompleted EventType = "validation.completed"
	EventValidationFailed    EventType = "validation.failed"
)

type Event struct {
	EventID   string
	Type      EventType
	RunID     string
	TaskID    string
	Timestamp time.Time
	Payload   map[string]any
}
