package kernel

import (
	"iter"

	"github.com/mossagents/moss/kernel/session"
)

// LoopAgent repeatedly runs a sub-agent until a stop condition is met or max iterations reached.
type LoopAgent struct {
	name        string
	description string
	subAgent    Agent
	maxIter     int
	shouldStop  func(*session.Event) bool
}

// LoopAgentConfig configures a LoopAgent.
type LoopAgentConfig struct {
	Name        string
	Description string
	SubAgent    Agent
	MaxIter     int                       // 0 defaults to 10
	ShouldStop  func(*session.Event) bool // optional stop condition
}

// NewLoopAgent creates a new loop agent that repeats execution of a sub-agent.
func NewLoopAgent(cfg LoopAgentConfig) *LoopAgent {
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = 10
	}
	return &LoopAgent{
		name:        cfg.Name,
		description: cfg.Description,
		subAgent:    cfg.SubAgent,
		maxIter:     cfg.MaxIter,
		shouldStop:  cfg.ShouldStop,
	}
}

// Name returns the agent's name.
func (a *LoopAgent) Name() string { return a.name }

// Description returns the agent's description.
func (a *LoopAgent) Description() string { return a.description }

// SubAgents returns the agent's sub-agent (the one being looped).
func (a *LoopAgent) SubAgents() []Agent { return []Agent{a.subAgent} }

// Run repeatedly executes the sub-agent, yielding all events.
func (a *LoopAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		subCtx := ctx.WithAgent(a.subAgent).WithBranch(ctx.Branch() + "." + a.subAgent.Name())

		for i := 0; i < a.maxIter; i++ {
			if ctx.Err() != nil || ctx.Ended() {
				return
			}

			var lastEvent *session.Event
			for event, err := range a.subAgent.Run(subCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				lastEvent = event
				if !yield(event, nil) {
					return
				}
			}

			if a.shouldStop != nil && lastEvent != nil && a.shouldStop(lastEvent) {
				return
			}
		}
	}
}
