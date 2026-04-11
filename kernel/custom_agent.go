package kernel

import (
	"iter"

	"github.com/mossagents/moss/kernel/session"
)

// CustomAgent wraps a user-provided function as an Agent.
type CustomAgent struct {
	name        string
	description string
	run         func(*InvocationContext) iter.Seq2[*session.Event, error]
	subAgents   []Agent
}

// CustomAgentConfig configures a CustomAgent.
type CustomAgentConfig struct {
	Name        string
	Description string
	Run         func(*InvocationContext) iter.Seq2[*session.Event, error]
	SubAgents   []Agent
}

// NewCustomAgent creates a new custom agent from a user-provided function.
func NewCustomAgent(cfg CustomAgentConfig) *CustomAgent {
	return &CustomAgent{
		name:        cfg.Name,
		description: cfg.Description,
		run:         cfg.Run,
		subAgents:   cfg.SubAgents,
	}
}

// Name returns the agent's name.
func (a *CustomAgent) Name() string { return a.name }

// Description returns the agent's description.
func (a *CustomAgent) Description() string { return a.description }

// SubAgents returns the agent's sub-agents.
func (a *CustomAgent) SubAgents() []Agent { return a.subAgents }

// Run delegates execution to the user-provided function.
func (a *CustomAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return a.run(ctx)
}
