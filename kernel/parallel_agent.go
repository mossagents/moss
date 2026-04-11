package kernel

import (
	"iter"
	"sync"

	"github.com/mossagents/moss/kernel/session"
)

// ParallelAgent runs its sub-agents concurrently and yields all collected events.
type ParallelAgent struct {
	name        string
	description string
	subAgents   []Agent
}

// NewParallelAgent creates a new parallel agent that runs sub-agents concurrently.
func NewParallelAgent(name, description string, subAgents ...Agent) *ParallelAgent {
	return &ParallelAgent{
		name:        name,
		description: description,
		subAgents:   subAgents,
	}
}

// Name returns the agent's name.
func (a *ParallelAgent) Name() string { return a.name }

// Description returns the agent's description.
func (a *ParallelAgent) Description() string { return a.description }

// SubAgents returns the agent's sub-agents.
func (a *ParallelAgent) SubAgents() []Agent { return a.subAgents }

// Run executes sub-agents concurrently, collecting events from all before yielding them.
func (a *ParallelAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		type agentResult struct {
			events []*session.Event
			err    error
		}
		results := make([]agentResult, len(a.subAgents))
		var wg sync.WaitGroup

		for i, sub := range a.subAgents {
			wg.Add(1)
			go func(idx int, agent Agent) {
				defer wg.Done()
				subCtx := ctx.WithAgent(agent).WithBranch(ctx.Branch() + "." + agent.Name())
				var events []*session.Event
				for event, err := range agent.Run(subCtx) {
					if err != nil {
						results[idx] = agentResult{err: err}
						return
					}
					events = append(events, event)
				}
				results[idx] = agentResult{events: events}
			}(i, sub)
		}

		wg.Wait()

		// Yield events in agent order (deterministic output).
		for _, r := range results {
			if r.err != nil {
				yield(nil, r.err)
				return
			}
			for _, event := range r.events {
				if !yield(event, nil) {
					return
				}
			}
		}
	}
}
