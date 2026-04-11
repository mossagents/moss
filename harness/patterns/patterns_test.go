package patterns

import (
	"fmt"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

// stubAgent is a minimal Agent implementation for testing.
type stubAgent struct {
	name   string
	events []*session.Event
	err    error
	delay  time.Duration
}

func (s *stubAgent) Name() string { return s.name }
func (s *stubAgent) Run(_ *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		for _, e := range s.events {
			if !yield(e, nil) {
				return
			}
		}
		if s.err != nil {
			yield(nil, s.err)
		}
	}
}

func makeEvent(author, text string) *session.Event {
	return &session.Event{
		ID:        fmt.Sprintf("evt-%s", author),
		Author:    author,
		Timestamp: time.Now(),
		Content: &model.Message{
			Role: model.RoleAssistant,
			ContentParts: []model.ContentPart{
				{Type: model.ContentPartText, Text: text},
			},
		},
	}
}

func testCtx() *kernel.InvocationContext {
	return kernel.NewInvocationContext(nil, kernel.InvocationContextParams{
		Branch: "test",
		Session: &session.Session{
			ID:    "test-session",
			State: make(map[string]any),
		},
	})
}

// --- Sequential ---

func TestSequentialAgent_RunsInOrder(t *testing.T) {
	var order []string
	a1 := &stubAgent{name: "a1", events: []*session.Event{makeEvent("a1", "first")}}
	a2 := &stubAgent{name: "a2", events: []*session.Event{makeEvent("a2", "second")}}

	seq := &SequentialAgent{
		AgentName: "seq",
		Agents:    []kernel.Agent{a1, a2},
	}

	for event, err := range seq.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		order = append(order, event.Author)
	}

	if len(order) != 2 || order[0] != "a1" || order[1] != "a2" {
		t.Fatalf("expected [a1, a2], got %v", order)
	}
}

func TestSequentialAgent_StopsOnError(t *testing.T) {
	a1 := &stubAgent{name: "a1", err: fmt.Errorf("boom")}
	a2 := &stubAgent{name: "a2", events: []*session.Event{makeEvent("a2", "never")}}

	seq := &SequentialAgent{AgentName: "seq", Agents: []kernel.Agent{a1, a2}}

	var count int
	for _, err := range seq.Run(testCtx()) {
		count++
		if err != nil {
			if err.Error() != "boom" {
				t.Fatalf("unexpected error: %v", err)
			}
			return
		}
	}
	t.Fatalf("expected error, got %d events", count)
}

func TestSequentialAgent_SubAgents(t *testing.T) {
	a1 := &stubAgent{name: "a1"}
	a2 := &stubAgent{name: "a2"}
	seq := &SequentialAgent{AgentName: "seq", Agents: []kernel.Agent{a1, a2}}
	if len(seq.SubAgents()) != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", len(seq.SubAgents()))
	}
}

// --- Parallel ---

func TestParallelAgent_RunsConcurrently(t *testing.T) {
	a1 := &stubAgent{name: "a1", events: []*session.Event{makeEvent("a1", "r1")}, delay: 10 * time.Millisecond}
	a2 := &stubAgent{name: "a2", events: []*session.Event{makeEvent("a2", "r2")}, delay: 10 * time.Millisecond}

	par := &ParallelAgent{
		AgentName: "par",
		Agents:    []kernel.Agent{a1, a2},
	}

	start := time.Now()
	var events []*session.Event
	for event, err := range par.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}
	elapsed := time.Since(start)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Parallel execution should take ~10ms, not ~20ms.
	if elapsed > 50*time.Millisecond {
		t.Fatalf("expected parallel execution, took %v", elapsed)
	}
}

func TestParallelAgent_CustomAggregator(t *testing.T) {
	a1 := &stubAgent{name: "a1", events: []*session.Event{makeEvent("a1", "r1")}}
	a2 := &stubAgent{name: "a2", events: []*session.Event{makeEvent("a2", "r2")}}

	var callCount atomic.Int32
	par := &ParallelAgent{
		AgentName: "par",
		Agents:    []kernel.Agent{a1, a2},
		Aggregator: func(results [][]session.Event) []*session.Event {
			callCount.Add(1)
			// Reverse order.
			var out []*session.Event
			for i := len(results) - 1; i >= 0; i-- {
				for j := range results[i] {
					out = append(out, &results[i][j])
				}
			}
			return out
		},
	}

	var order []string
	for event, err := range par.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		order = append(order, event.Author)
	}

	if callCount.Load() != 1 {
		t.Fatalf("expected aggregator called once, got %d", callCount.Load())
	}
	if len(order) != 2 || order[0] != "a2" || order[1] != "a1" {
		t.Fatalf("expected reversed order [a2, a1], got %v", order)
	}
}

func TestParallelAgent_PropagatesError(t *testing.T) {
	a1 := &stubAgent{name: "a1", err: fmt.Errorf("fail")}
	a2 := &stubAgent{name: "a2", events: []*session.Event{makeEvent("a2", "ok")}}

	par := &ParallelAgent{AgentName: "par", Agents: []kernel.Agent{a1, a2}}

	var gotErr error
	for _, err := range par.Run(testCtx()) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error from parallel agent")
	}
}

func TestParallelAgent_EmptyAgents(t *testing.T) {
	par := &ParallelAgent{AgentName: "par", Agents: nil}
	var count int
	for _, err := range par.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 events for empty parallel, got %d", count)
	}
}

// --- Loop ---

func TestLoopAgent_MaxIterations(t *testing.T) {
	a := &stubAgent{name: "worker", events: []*session.Event{makeEvent("worker", "tick")}}
	loop := &LoopAgent{
		AgentName:     "loop",
		Agent:         a,
		MaxIterations: 3,
	}

	var count int
	for _, err := range loop.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 events (3 iterations), got %d", count)
	}
}

func TestLoopAgent_ExitCondition(t *testing.T) {
	a := &stubAgent{name: "worker", events: []*session.Event{makeEvent("worker", "tick")}}
	loop := &LoopAgent{
		AgentName:     "loop",
		Agent:         a,
		MaxIterations: 10,
		ShouldExit: func(_ []session.Event, iteration int) bool {
			return iteration >= 2 // exit after 3rd iteration (0,1,2)
		},
	}

	var count int
	for _, err := range loop.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 events (exit at iteration 2), got %d", count)
	}
}

func TestLoopAgent_SubAgents(t *testing.T) {
	a := &stubAgent{name: "worker"}
	loop := &LoopAgent{AgentName: "loop", Agent: a}
	if len(loop.SubAgents()) != 1 || loop.SubAgents()[0].Name() != "worker" {
		t.Fatalf("expected [worker], got %v", loop.SubAgents())
	}
}

// --- Supervisor ---

func TestSupervisorAgent_RoutesToWorker(t *testing.T) {
	w1 := &stubAgent{name: "w1", events: []*session.Event{makeEvent("w1", "result")}}
	w2 := &stubAgent{name: "w2", events: []*session.Event{makeEvent("w2", "other")}}

	sup := &SupervisorAgent{
		AgentName: "sup",
		Workers:   []kernel.Agent{w1, w2},
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			return workers[0] // always pick first
		},
	}

	var authors []string
	for event, err := range sup.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		authors = append(authors, event.Author)
	}

	if len(authors) != 1 || authors[0] != "w1" {
		t.Fatalf("expected [w1], got %v", authors)
	}
}

func TestSupervisorAgent_NoMatchReturnsEmpty(t *testing.T) {
	w1 := &stubAgent{name: "w1", events: []*session.Event{makeEvent("w1", "result")}}

	sup := &SupervisorAgent{
		AgentName: "sup",
		Workers:   []kernel.Agent{w1},
		Router: func(_ *kernel.InvocationContext, _ []kernel.Agent) kernel.Agent {
			return nil
		},
	}

	var count int
	for _, err := range sup.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 events when no worker selected, got %d", count)
	}
}

// --- Composition ---

func TestComposition_SequentialWithParallel(t *testing.T) {
	a1 := &stubAgent{name: "a1", events: []*session.Event{makeEvent("a1", "prep")}}
	b1 := &stubAgent{name: "b1", events: []*session.Event{makeEvent("b1", "r1")}}
	b2 := &stubAgent{name: "b2", events: []*session.Event{makeEvent("b2", "r2")}}
	c1 := &stubAgent{name: "c1", events: []*session.Event{makeEvent("c1", "final")}}

	workflow := &SequentialAgent{
		AgentName: "workflow",
		Agents: []kernel.Agent{
			a1,
			&ParallelAgent{AgentName: "gather", Agents: []kernel.Agent{b1, b2}},
			c1,
		},
	}

	var authors []string
	for event, err := range workflow.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		authors = append(authors, event.Author)
	}

	// a1 first, then b1+b2 (order may vary but both present), then c1
	if len(authors) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(authors), authors)
	}
	if authors[0] != "a1" || authors[3] != "c1" {
		t.Fatalf("expected a1 first and c1 last, got %v", authors)
	}
}

// --- Research ---

func TestResearchAgent_BasicFlow(t *testing.T) {
	queryAgent := &stubAgent{
		name:   "query",
		events: []*session.Event{makeEvent("query", "query1\nquery2")},
	}
	searchAgent := &stubAgent{
		name:   "search",
		events: []*session.Event{makeEvent("search", "finding")},
	}
	synthAgent := &stubAgent{
		name:   "synth",
		events: []*session.Event{makeEvent("synth", "answer")},
	}

	research := NewResearchAgent(ResearchConfig{
		Name:           "research",
		QueryAgent:     queryAgent,
		SearchAgent:    searchAgent,
		SynthesisAgent: synthAgent,
		MaxIterations:  1,
	})

	var authors []string
	for event, err := range research.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		authors = append(authors, event.Author)
	}

	// query → search (×2 parallel) → synthesis
	if len(authors) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(authors), authors)
	}
	if authors[0] != "query" {
		t.Fatalf("expected query first, got %v", authors[0])
	}
	if authors[len(authors)-1] != "synth" {
		t.Fatalf("expected synth last, got %v", authors[len(authors)-1])
	}
}

func TestResearchAgent_MultiIteration(t *testing.T) {
	queryAgent := &stubAgent{name: "q", events: []*session.Event{makeEvent("q", "q1")}}
	searchAgent := &stubAgent{name: "s", events: []*session.Event{makeEvent("s", "f1")}}
	synthAgent := &stubAgent{name: "syn", events: []*session.Event{makeEvent("syn", "a1")}}

	research := NewResearchAgent(ResearchConfig{
		Name:           "research",
		QueryAgent:     queryAgent,
		SearchAgent:    searchAgent,
		SynthesisAgent: synthAgent,
		MaxIterations:  3,
		QualityCheck: func(_ []session.Event, iteration int) bool {
			return iteration >= 1 // accept on second iteration
		},
	})

	var count int
	for _, err := range research.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	// 2 iterations × (query + search + synth = 3) = 6
	if count != 6 {
		t.Fatalf("expected 6 events (2 iterations), got %d", count)
	}
}

func TestConcatAggregate(t *testing.T) {
	results := [][]session.Event{
		{*makeEvent("a", "1")},
		{*makeEvent("b", "2"), *makeEvent("b", "3")},
	}
	merged := ConcatAggregate(results)
	if len(merged) != 3 {
		t.Fatalf("expected 3 events, got %d", len(merged))
	}
}
