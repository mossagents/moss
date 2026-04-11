package patterns

import (
	"fmt"
	"iter"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// SequentialAgent executes a list of sub-agents one after another in a
// deterministic order. The output of each agent is appended to the shared
// session before the next agent runs, so later agents can build on earlier
// results.
//
// If any sub-agent returns an error, the sequence aborts and the error
// propagates to the caller.
type SequentialAgent struct {
	AgentName string
	Desc      string
	Agents    []kernel.Agent
}

var _ kernel.Agent = (*SequentialAgent)(nil)
var _ kernel.AgentWithDescription = (*SequentialAgent)(nil)
var _ kernel.AgentWithSubAgents = (*SequentialAgent)(nil)

func (s *SequentialAgent) Name() string        { return s.AgentName }
func (s *SequentialAgent) Description() string { return s.Desc }
func (s *SequentialAgent) SubAgents() []kernel.Agent { return s.Agents }

func (s *SequentialAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		for i, agent := range s.Agents {
			if ctx.Context != nil && ctx.Err() != nil || ctx.Ended() {
				return
			}
			childCtx := ctx.WithAgent(agent).
				WithBranch(fmt.Sprintf("%s.%s[%d]", ctx.Branch(), agent.Name(), i))

			for event, err := range agent.Run(childCtx) {
				if !yield(event, err) {
					return
				}
				if err != nil {
					return
				}
				if event != nil && (event.Actions.TransferToAgent != "" || event.Actions.Escalate) {
					return
				}
			}
		}
	}
}
