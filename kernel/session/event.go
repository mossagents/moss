package session

import (
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// EventType indicates the kind of event produced during agent execution.
type EventType string

const (
	// EventTypeLLMResponse indicates an LLM response event (assistant message).
	EventTypeLLMResponse EventType = "llm_response"
	// EventTypeToolResult indicates a tool execution result event.
	EventTypeToolResult EventType = "tool_result"
	// EventTypeCustom indicates a custom agent event.
	EventTypeCustom EventType = "custom"
)

// Event represents a unit of change produced by an agent during execution.
// Events are yielded by Agent.Run() and form the primary output of agent execution.
type Event struct {
	// Type indicates the kind of event (llm_response, tool_result, custom).
	Type EventType `json:"type,omitempty"`

	// ID uniquely identifies this event within a session.
	ID string `json:"id"`

	// Author is the name of the agent that produced this event.
	Author string `json:"author"`

	// Content is the message content (LLM response, tool result, etc.).
	Content *model.Message `json:"content,omitempty"`

	// Partial indicates this is a streaming partial event (incomplete content).
	Partial bool `json:"partial,omitempty"`

	// Actions describes state mutations and control flow changes.
	Actions EventActions `json:"actions,omitempty"`

	// Usage records token consumption for this event.
	Usage model.TokenUsage `json:"usage,omitempty"`

	// TurnID associates this event with a specific turn.
	TurnID string `json:"turn_id,omitempty"`

	// Timestamp when the event was created.
	Timestamp time.Time `json:"timestamp"`
}

// EventActions describes state mutations and control flow requests within an event.
type EventActions struct {
	// StateDelta contains session state changes to apply.
	StateDelta map[string]any `json:"state_delta,omitempty"`

	// MaterializedIn identifies the materialization domain where the event's
	// shared effects were last committed. This allows the same event to be
	// committed once per outward session domain (e.g. child branch -> parent ->
	// root) without relying on a single global boolean.
	MaterializedIn string `json:"materialized_in,omitempty"`

	// TransferToAgent requests transferring execution to another agent.
	TransferToAgent string `json:"transfer_to_agent,omitempty"`

	// Escalate requests returning control to the parent agent.
	Escalate bool `json:"escalate,omitempty"`
}
