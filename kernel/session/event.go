package session

import (
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// Event represents a unit of change produced by an agent during execution.
// Events are yielded by Agent.Run() and form the primary output of agent execution.
type Event struct {
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

	// TransferToAgent requests transferring execution to another agent.
	TransferToAgent string `json:"transfer_to_agent,omitempty"`

	// Escalate requests returning control to the parent agent.
	Escalate bool `json:"escalate,omitempty"`
}
