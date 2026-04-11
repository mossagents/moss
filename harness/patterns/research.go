package patterns

import (
	"fmt"
	"iter"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

// ResearchConfig configures a research orchestration pattern.
type ResearchConfig struct {
	// Name is the unique agent name.
	Name string
	// Description describes the research agent.
	Description string
	// QueryAgent decomposes a user question into research queries.
	QueryAgent kernel.Agent
	// SearchAgent executes individual search queries and returns raw findings.
	SearchAgent kernel.Agent
	// SynthesisAgent synthesises search results into a coherent answer.
	SynthesisAgent kernel.Agent
	// MaxIterations caps the query→search→evaluate cycle. Default 3.
	MaxIterations int
	// MaxParallelSearches caps concurrent SearchAgent invocations per cycle.
	// Default is the number of queries produced by QueryAgent.
	MaxParallelSearches int
	// QualityCheck evaluates synthesis output to decide whether another
	// research iteration is needed. Return true to accept the answer and
	// stop iterating. If nil, the loop runs for exactly one iteration.
	QualityCheck func(events []session.Event, iteration int) bool
}

// ResearchAgent implements an iterative research pattern:
//
//	Query → Parallel Search → Synthesis → (optional quality check) → repeat
//
// It composes SequentialAgent, ParallelAgent, and LoopAgent internally.
type ResearchAgent struct {
	cfg ResearchConfig
}

var _ kernel.Agent = (*ResearchAgent)(nil)
var _ kernel.AgentWithDescription = (*ResearchAgent)(nil)
var _ kernel.AgentWithSubAgents = (*ResearchAgent)(nil)

// NewResearchAgent creates a ResearchAgent with the given configuration.
func NewResearchAgent(cfg ResearchConfig) *ResearchAgent {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 3
	}
	if cfg.QualityCheck == nil {
		cfg.QualityCheck = func(_ []session.Event, _ int) bool { return true }
	}
	return &ResearchAgent{cfg: cfg}
}

func (r *ResearchAgent) Name() string        { return r.cfg.Name }
func (r *ResearchAgent) Description() string { return r.cfg.Description }
func (r *ResearchAgent) SubAgents() []kernel.Agent {
	return []kernel.Agent{r.cfg.QueryAgent, r.cfg.SearchAgent, r.cfg.SynthesisAgent}
}

func (r *ResearchAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		for iteration := 0; iteration < r.cfg.MaxIterations; iteration++ {
			// Phase 1: Query decomposition
			queryBranch := fmt.Sprintf("%s.query[%d]", ctx.Branch(), iteration)
			queryCtx := ctx.WithAgent(r.cfg.QueryAgent).WithBranch(queryBranch)

			var queryEvents []session.Event
			for event, err := range r.cfg.QueryAgent.Run(queryCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				if event != nil {
					queryEvents = append(queryEvents, *event)
					if !yield(event, nil) {
						return
					}
				}
			}

			// Phase 2: Parallel search (one SearchAgent invocation per query)
			queries := extractQueries(queryEvents)
			if len(queries) == 0 {
				queries = []string{"search"} // fallback
			}

			maxPar := r.cfg.MaxParallelSearches
			if maxPar <= 0 || maxPar > len(queries) {
				maxPar = len(queries)
			}

			// Create parallel search agents for each query batch
			searchAgents := make([]kernel.Agent, 0, maxPar)
			for i := 0; i < maxPar && i < len(queries); i++ {
				searchAgents = append(searchAgents, r.cfg.SearchAgent)
			}
			par := &ParallelAgent{
				AgentName: fmt.Sprintf("%s-search-batch-%d", r.cfg.Name, iteration),
				Agents:    searchAgents,
			}
			searchBranch := fmt.Sprintf("%s.search[%d]", ctx.Branch(), iteration)
			searchCtx := ctx.WithAgent(par).WithBranch(searchBranch)

			for event, err := range par.Run(searchCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				if event != nil {
					if !yield(event, nil) {
						return
					}
				}
			}

			// Phase 3: Synthesis
			synthBranch := fmt.Sprintf("%s.synthesis[%d]", ctx.Branch(), iteration)
			synthCtx := ctx.WithAgent(r.cfg.SynthesisAgent).WithBranch(synthBranch)

			var synthEvents []session.Event
			for event, err := range r.cfg.SynthesisAgent.Run(synthCtx) {
				if err != nil {
					yield(nil, err)
					return
				}
				if event != nil {
					synthEvents = append(synthEvents, *event)
					if !yield(event, nil) {
						return
					}
				}
			}

			if ctx.Ended() {
				return
			}

			// Quality check
			if r.cfg.QualityCheck(synthEvents, iteration) {
				return
			}
		}
	}
}

// extractQueries looks through events for content that could serve as
// search queries. It splits text content by newlines and trims whitespace.
func extractQueries(events []session.Event) []string {
	var queries []string
	for _, e := range events {
		if e.Content == nil {
			continue
		}
		for _, part := range e.Content.ContentParts {
			if part.Type == model.ContentPartText && part.Text != "" {
				for _, line := range strings.Split(part.Text, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						queries = append(queries, line)
					}
				}
			}
		}
	}
	return queries
}
