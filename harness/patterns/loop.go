package patterns

import (
	"fmt"
	"iter"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// ExitFunc evaluates events produced by an iteration to determine whether
// the loop should stop. It receives the events from the latest iteration
// and the current iteration index (0-based). Return true to exit the loop.
type ExitFunc func(events []session.Event, iteration int) bool

// LoopAgent repeatedly executes a sub-agent until an exit condition is met
// or MaxIterations is reached.
//
// Events from every iteration are yielded to the caller. The loop exits when:
//   - ShouldExit returns true for the latest batch of events, OR
//   - MaxIterations > 0 and the iteration count reaches it, OR
//   - the sub-agent signals EndInvocation, OR
//   - the consumer stops pulling events.
type LoopAgent struct {
	AgentName     string
	Desc          string
	Agent         kernel.Agent
	MaxIterations int
	ShouldExit    ExitFunc
}

var _ kernel.Agent = (*LoopAgent)(nil)
var _ kernel.AgentWithDescription = (*LoopAgent)(nil)
var _ kernel.AgentWithSubAgents = (*LoopAgent)(nil)

func (l *LoopAgent) Name() string        { return l.AgentName }
func (l *LoopAgent) Description() string { return l.Desc }
func (l *LoopAgent) SubAgents() []kernel.Agent {
	if l.Agent == nil {
		return nil
	}
	return []kernel.Agent{l.Agent}
}

func (l *LoopAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		for iteration := 0; ; iteration++ {
			if l.MaxIterations > 0 && iteration >= l.MaxIterations {
				return
			}

			childCtx := ctx.WithAgent(l.Agent).
				WithBranch(fmt.Sprintf("%s.%s[iter=%d]", ctx.Branch(), l.Agent.Name(), iteration))

			var iterEvents []session.Event
			for event, err := range l.Agent.Run(childCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				if event != nil {
					iterEvents = append(iterEvents, *event)
					if !yield(event, nil) {
						return
					}
				}
			}

			if ctx.Ended() {
				return
			}

			if l.ShouldExit != nil && l.ShouldExit(iterEvents, iteration) {
				return
			}
		}
	}
}
