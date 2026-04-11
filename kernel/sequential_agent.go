package kernel

import (
	"iter"

	"github.com/mossagents/moss/kernel/session"
)

// SequentialAgent runs its sub-agents in sequence, forwarding events from each.
type SequentialAgent struct {
	name        string
	description string
	subAgents   []Agent
}

// NewSequentialAgent creates a new sequential agent that runs sub-agents in order.
func NewSequentialAgent(name, description string, subAgents ...Agent) *SequentialAgent {
	return &SequentialAgent{
		name:        name,
		description: description,
		subAgents:   subAgents,
	}
}

// Name returns the agent's name.
func (a *SequentialAgent) Name() string { return a.name }

// Description returns the agent's description.
func (a *SequentialAgent) Description() string { return a.description }

// SubAgents returns the agent's sub-agents.
func (a *SequentialAgent) SubAgents() []Agent { return a.subAgents }

// Run executes sub-agents sequentially, yielding all events.
func (a *SequentialAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		for _, sub := range a.subAgents {
			if ctx.Err() != nil || ctx.Ended() {
				return
			}
			subCtx := ctx.WithAgent(sub).WithBranch(ctx.Branch() + "." + sub.Name())
			for event, err := range sub.Run(subCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(event, nil) {
					return
				}
				if event.Actions.TransferToAgent != "" || event.Actions.Escalate {
					return
				}
			}
		}
	}
}
