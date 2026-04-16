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
	// MaxParallelSearches caps concurrent SearchAgent invocations per search batch.
	// All queries are still processed; batches larger than this cap are executed
	// sequentially.
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
// It composes SequentialAgent, ParallelAgent, and LoopAgent internally, while
// passing explicit query input to search branches and explicit findings input
// to synthesis branches. All three phases run on branch-local sessions and
// commit yielded events back into the parent session via the shared
// materialization contract.
type ResearchAgent struct {
	cfg ResearchConfig
}

const (
	researchQueryStateKey      = "patterns.research.query"
	researchQueriesStateKey    = "patterns.research.queries"
	researchQueryIndexStateKey = "patterns.research.query_index"
	researchIterationStateKey  = "patterns.research.iteration"
	researchFindingsStateKey   = "patterns.research.findings"
)

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
		if r.cfg.QueryAgent == nil || r.cfg.SearchAgent == nil || r.cfg.SynthesisAgent == nil {
			yield(nil, fmt.Errorf("research agent requires query, search, and synthesis agents"))
			return
		}
		for iteration := 0; iteration < r.cfg.MaxIterations; iteration++ {
			// Phase 1: Query decomposition
			queryBranch := fmt.Sprintf("%s.query[%d]", ctx.Branch(), iteration)
			var queryEvents []session.Event
			for event, err := range ctx.RunChild(r.cfg.QueryAgent, kernel.ChildRunConfig{
				Branch: queryBranch,
			}) {
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
				queries = defaultResearchQueries(ctx)
			}

			maxPar := r.cfg.MaxParallelSearches
			if maxPar <= 0 || maxPar > len(queries) {
				maxPar = len(queries)
			}

			var searchEvents []session.Event
			for batchStart := 0; batchStart < len(queries); batchStart += maxPar {
				batchEnd := batchStart + maxPar
				if batchEnd > len(queries) {
					batchEnd = len(queries)
				}

				searchAgents := make([]kernel.Agent, 0, batchEnd-batchStart)
				for queryIndex := batchStart; queryIndex < batchEnd; queryIndex++ {
					queryText := queries[queryIndex]
					queryOrdinal := queryIndex
					searchAgents = append(searchAgents, newResearchInputAgent(
						r.cfg.SearchAgent,
						textUserMessage(queryText),
						func(sess *session.Session) {
							if sess == nil {
								return
							}
							sess.SetState(researchQueryStateKey, queryText)
							sess.SetState(researchQueryIndexStateKey, queryOrdinal)
							sess.SetState(researchIterationStateKey, iteration)
						},
					))
				}
				par := &ParallelAgent{
					AgentName: fmt.Sprintf("%s-search-batch-%d", r.cfg.Name, iteration),
					Agents:    searchAgents,
				}
				searchBranch := fmt.Sprintf("%s.search[%d].batch[%d]", ctx.Branch(), iteration, batchStart/maxPar)

				for event, err := range ctx.RunChild(par, kernel.ChildRunConfig{
					Branch: searchBranch,
				}) {
					if err != nil {
						yield(nil, err)
						return
					}
					if event != nil {
						searchEvents = append(searchEvents, *event)
						if !yield(event, nil) {
							return
						}
					}
				}
			}

			// Phase 3: Synthesis
			synthBranch := fmt.Sprintf("%s.synthesis[%d]", ctx.Branch(), iteration)
			findings := extractFindings(searchEvents)
			synthInput := textUserMessage(buildSynthesisInput(queries, findings))
			var synthEvents []session.Event
			for event, err := range ctx.RunChild(r.cfg.SynthesisAgent, kernel.ChildRunConfig{
				Branch:      synthBranch,
				UserContent: synthInput,
				PrepareSession: func(sess *session.Session) {
					if sess == nil {
						return
					}
					sess.SetState(researchQueriesStateKey, append([]string(nil), queries...))
					sess.SetState(researchFindingsStateKey, append([]string(nil), findings...))
					sess.SetState(researchIterationStateKey, iteration)
				},
			}) {
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

func defaultResearchQueries(ctx *kernel.InvocationContext) []string {
	if ctx != nil && ctx.UserContent() != nil {
		if text := strings.TrimSpace(model.ContentPartsToPlainText(ctx.UserContent().ContentParts)); text != "" {
			return []string{text}
		}
	}
	if ctx != nil && ctx.Session() != nil {
		if goal := strings.TrimSpace(ctx.Session().Config.Goal); goal != "" {
			return []string{goal}
		}
	}
	return []string{"search"}
}

func extractFindings(events []session.Event) []string {
	findings := make([]string, 0, len(events))
	for _, event := range events {
		if text := strings.TrimSpace(eventText(event)); text != "" {
			findings = append(findings, text)
		}
	}
	return findings
}

func buildSynthesisInput(queries, findings []string) string {
	var sections []string
	if len(queries) > 0 {
		lines := make([]string, 0, len(queries)+1)
		lines = append(lines, "Research queries:")
		for _, query := range queries {
			lines = append(lines, "- "+query)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(findings) > 0 {
		lines := make([]string, 0, len(findings)+1)
		lines = append(lines, "Search findings:")
		for i, finding := range findings {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, finding))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

type researchInputAgent struct {
	agent          kernel.Agent
	input          *model.Message
	prepareSession func(*session.Session)
}

func newResearchInputAgent(agent kernel.Agent, input *model.Message, prepare func(*session.Session)) kernel.Agent {
	return &researchInputAgent{
		agent:          agent,
		input:          input,
		prepareSession: prepare,
	}
}

func (a *researchInputAgent) Name() string {
	if a.agent == nil {
		return "research-input"
	}
	return a.agent.Name()
}

func (a *researchInputAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		if a.agent == nil {
			return
		}
		for event, err := range ctx.RunChild(a.agent, kernel.ChildRunConfig{
			UserContent:    a.input,
			PrepareSession: a.prepareSession,
		}) {
			if !yield(event, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}
