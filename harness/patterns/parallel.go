package patterns

import (
	"fmt"
	"iter"
	"sync"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// AggregateFunc combines events from multiple parallel agents into a final
// stream of events. The input is a slice of event slices (one per agent,
// in the same order as the Agents field). The output is the merged event
// sequence.
type AggregateFunc func(agentResults [][]session.Event) []*session.Event

// ParallelAgent executes multiple sub-agents concurrently and merges their
// results using an AggregateFunc.
//
// Each sub-agent receives a copy of the current session state but operates
// in isolation. Their events are collected and then passed to the Aggregator
// for merging. The merged event stream is then materialized back into the
// parent session in merged order. If no Aggregator is set, events are
// concatenated in agent order.
//
// If any sub-agent returns an error, all other goroutines are allowed to
// finish but the first error is propagated to the caller.
type ParallelAgent struct {
	AgentName  string
	Desc       string
	Agents     []kernel.Agent
	Aggregator AggregateFunc
}

var _ kernel.Agent = (*ParallelAgent)(nil)
var _ kernel.AgentWithDescription = (*ParallelAgent)(nil)
var _ kernel.AgentWithSubAgents = (*ParallelAgent)(nil)

func (p *ParallelAgent) Name() string              { return p.AgentName }
func (p *ParallelAgent) Description() string       { return p.Desc }
func (p *ParallelAgent) SubAgents() []kernel.Agent { return p.Agents }

type agentResult struct {
	index  int
	events []session.Event
	err    error
}

func (p *ParallelAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		n := len(p.Agents)
		if n == 0 {
			return
		}

		results := make([]agentResult, n)
		var wg sync.WaitGroup
		wg.Add(n)

		for i, agent := range p.Agents {
			go func(idx int, a kernel.Agent) {
				defer wg.Done()
				var events []session.Event
				var firstErr error
				for event, err := range ctx.RunChild(a, kernel.ChildRunConfig{
					Branch:                 fmt.Sprintf("%s.%s[%d]", ctx.Branch(), a.Name(), idx),
					DisableMaterialization: true,
				}) {
					if err != nil {
						firstErr = err
						break
					}
					if event != nil {
						events = append(events, *event)
					}
				}
				results[idx] = agentResult{index: idx, events: events, err: firstErr}
			}(i, agent)
		}
		wg.Wait()

		// Check for errors.
		for _, r := range results {
			if r.err != nil {
				yield(nil, r.err)
				return
			}
		}

		// Aggregate results.
		if p.Aggregator != nil {
			agentEvents := make([][]session.Event, n)
			for i, r := range results {
				agentEvents[i] = r.events
			}
			for _, event := range p.Aggregator(agentEvents) {
				session.MaterializeEvent(ctx.Session(), event)
				if !yield(event, nil) {
					return
				}
			}
			return
		}

		// Default: concatenate in agent order.
		for _, r := range results {
			for i := range r.events {
				session.MaterializeEvent(ctx.Session(), &r.events[i])
				if !yield(&r.events[i], nil) {
					return
				}
			}
		}
	}
}

// ConcatAggregate is the default AggregateFunc that concatenates events
// from all agents in order.
func ConcatAggregate(agentResults [][]session.Event) []*session.Event {
	var out []*session.Event
	for _, events := range agentResults {
		for i := range events {
			out = append(out, &events[i])
		}
	}
	return out
}
