package patterns

import (
	"fmt"
	"iter"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// RoutingStrategy decides which worker agent should handle the current
// invocation. It receives the invocation context and a list of available
// workers, and returns the selected worker or nil if none applies.
type RoutingStrategy func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent

// SupervisorAgent dynamically delegates work to specialized worker agents
// using a configurable RoutingStrategy. It models the Leader-Worker pattern
// commonly used in multi-agent systems.
//
// The supervisor itself does not produce events — it routes to a single
// worker and forwards that worker's event stream. If the routing strategy
// returns nil, the supervisor yields no events.
//
// For more sophisticated multi-step delegation (e.g., decomposing a task,
// distributing sub-tasks, and aggregating results), compose SupervisorAgent
// with SequentialAgent, ParallelAgent, and LoopAgent.
type SupervisorAgent struct {
	AgentName string
	Desc      string
	Workers   []kernel.Agent
	Router    RoutingStrategy
}

var _ kernel.Agent = (*SupervisorAgent)(nil)
var _ kernel.AgentWithDescription = (*SupervisorAgent)(nil)
var _ kernel.AgentWithSubAgents = (*SupervisorAgent)(nil)

func (s *SupervisorAgent) Name() string        { return s.AgentName }
func (s *SupervisorAgent) Description() string { return s.Desc }
func (s *SupervisorAgent) SubAgents() []kernel.Agent { return s.Workers }

func (s *SupervisorAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		selected := s.Router(ctx, s.Workers)
		if selected == nil {
			return
		}

		childCtx := ctx.WithAgent(selected).
			WithBranch(fmt.Sprintf("%s.%s", ctx.Branch(), selected.Name()))

		for event, err := range selected.Run(childCtx) {
			if !yield(event, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// RoundRobinRouter returns a RoutingStrategy that cycles through workers
// based on the iteration state stored in the session.
func RoundRobinRouter(stateKey string) RoutingStrategy {
	return func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
		if len(workers) == 0 {
			return nil
		}
		sess := ctx.Session()
		idx := 0
		if v, ok := sess.State[stateKey]; ok {
			if n, ok := v.(int); ok {
				idx = n
			}
		}
		selected := workers[idx%len(workers)]
		sess.State[stateKey] = (idx + 1) % len(workers)
		return selected
	}
}

// FirstMatchRouter returns a RoutingStrategy that delegates to the first
// worker accepted by the given predicate.
func FirstMatchRouter(match func(ctx *kernel.InvocationContext, w kernel.Agent) bool) RoutingStrategy {
	return func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
		for _, w := range workers {
			if match(ctx, w) {
				return w
			}
		}
		return nil
	}
}
