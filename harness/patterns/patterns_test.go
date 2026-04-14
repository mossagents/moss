package patterns

import (
	"fmt"
	"iter"
	"sort"
	"strings"
	"sync"
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
	onRun  func(*kernel.InvocationContext)
}

func (s *stubAgent) Name() string { return s.name }
func (s *stubAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		if s.onRun != nil {
			s.onRun(ctx)
		}
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

type scriptedAgent struct {
	name string
	run  func(*kernel.InvocationContext) iter.Seq2[*session.Event, error]
}

func (s *scriptedAgent) Name() string { return s.name }
func (s *scriptedAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return s.run(ctx)
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
	return testCtxWithSession(&session.Session{
		ID:    "test-session",
		State: make(map[string]any),
	})
}

func testCtxWithSession(sess *session.Session) *kernel.InvocationContext {
	return kernel.NewInvocationContext(nil, kernel.InvocationContextParams{
		Branch:  "test",
		Session: sess,
	})
}

func sessionMessageTexts(sess *session.Session) []string {
	msgs := sess.CopyMessages()
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, strings.TrimSpace(model.ContentPartsToPlainText(msg.ContentParts)))
	}
	return out
}

func supervisorDecisionFromSession(t *testing.T, sess *session.Session, key string) SupervisorDecision {
	t.Helper()
	actual, ok := sess.GetState(key)
	if !ok {
		t.Fatalf("expected supervisor decision state %q to be recorded", key)
	}
	decision, ok := actual.(SupervisorDecision)
	if !ok {
		t.Fatalf("decision type = %T, want SupervisorDecision", actual)
	}
	return decision
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

func TestSequentialAgent_MaterializesEventsIntoParentSession(t *testing.T) {
	parent := &session.Session{ID: "parent", State: map[string]any{}}
	first := makeEvent("a1", "first")
	first.Actions.StateDelta = map[string]any{"phase": "first"}
	a1 := &stubAgent{name: "a1", events: []*session.Event{first}}
	var (
		seenState any
		seenText  string
	)
	a2 := &scriptedAgent{
		name: "a2",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				seenState, _ = ctx.Session().GetState("phase")
				msgs := ctx.Session().CopyMessages()
				if len(msgs) > 0 {
					seenText = model.ContentPartsToPlainText(msgs[len(msgs)-1].ContentParts)
				}
				yield(makeEvent("a2", "second"), nil)
			}
		},
	}

	seq := &SequentialAgent{
		AgentName: "seq",
		Agents:    []kernel.Agent{a1, a2},
	}

	for _, err := range seq.Run(testCtxWithSession(parent)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if seenState != "first" {
		t.Fatalf("second agent saw phase = %v, want first", seenState)
	}
	if seenText != "first" {
		t.Fatalf("second agent saw last message = %q, want first", seenText)
	}
	texts := sessionMessageTexts(parent)
	if len(texts) != 2 || texts[0] != "first" || texts[1] != "second" {
		t.Fatalf("parent session messages = %v, want [first second]", texts)
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

func TestLoopAgent_MaterializesIterationEvents(t *testing.T) {
	parent := &session.Session{ID: "parent", State: map[string]any{}}
	var seenCounts []int
	worker := &scriptedAgent{
		name: "worker",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				seenCounts = append(seenCounts, len(ctx.Session().CopyMessages()))
				yield(makeEvent("worker", fmt.Sprintf("tick-%d", len(seenCounts))), nil)
			}
		},
	}
	loop := &LoopAgent{
		AgentName:     "loop",
		Agent:         worker,
		MaxIterations: 3,
	}

	for _, err := range loop.Run(testCtxWithSession(parent)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if got := fmt.Sprint(seenCounts); got != "[0 1 2]" {
		t.Fatalf("iteration sessions saw message counts %v, want [0 1 2]", seenCounts)
	}
	texts := sessionMessageTexts(parent)
	if len(texts) != 3 || texts[0] != "tick-1" || texts[1] != "tick-2" || texts[2] != "tick-3" {
		t.Fatalf("parent session messages = %v, want [tick-1 tick-2 tick-3]", texts)
	}
}

// --- Supervisor ---

func TestSupervisorAgent_RoutesToWorker(t *testing.T) {
	result := makeEvent("w1", "result")
	result.Actions.StateDelta = map[string]any{"handled_by": "w1"}
	w1 := &stubAgent{name: "w1", events: []*session.Event{result}}
	w2 := &stubAgent{name: "w2", events: []*session.Event{makeEvent("w2", "other")}}
	parent := &session.Session{ID: "parent", State: map[string]any{}}

	sup := &SupervisorAgent{
		AgentName: "sup",
		Workers:   []kernel.Agent{w1, w2},
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			return workers[0] // always pick first
		},
	}

	var authors []string
	for event, err := range sup.Run(testCtxWithSession(parent)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		authors = append(authors, event.Author)
	}

	if len(authors) != 1 || authors[0] != "w1" {
		t.Fatalf("expected [w1], got %v", authors)
	}
	if handledBy, _ := parent.GetState("handled_by"); handledBy != "w1" {
		t.Fatalf("parent state handled_by = %v, want w1", handledBy)
	}
	texts := sessionMessageTexts(parent)
	if len(texts) != 1 || texts[0] != "result" {
		t.Fatalf("parent session messages = %v, want [result]", texts)
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

func TestSupervisorAgent_FailoverRecordsDecisionState(t *testing.T) {
	sess := &session.Session{ID: "s1", State: map[string]any{}}
	w1 := &stubAgent{name: "w1", err: fmt.Errorf("boom")}
	w2 := &stubAgent{name: "w2", events: []*session.Event{makeEvent("w2", "ok")}}

	sup := &SupervisorAgent{
		AgentName:       "sup",
		Workers:         []kernel.Agent{w1, w2},
		FailoverOnError: true,
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			if len(workers) == 0 {
				return nil
			}
			return workers[0]
		},
	}

	var authors []string
	for event, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event != nil {
			authors = append(authors, event.Author)
		}
	}

	if len(authors) != 1 || authors[0] != "w2" {
		t.Fatalf("expected fallback to w2, got %v", authors)
	}
	actual, ok := sess.GetState("patterns.supervisor.sup")
	if !ok {
		t.Fatal("expected supervisor decision state to be recorded")
	}
	decision, ok := actual.(SupervisorDecision)
	if !ok {
		t.Fatalf("decision type = %T, want SupervisorDecision", actual)
	}
	if decision.Status != SupervisorStatusCompleted {
		t.Fatalf("status = %q, want %q", decision.Status, SupervisorStatusCompleted)
	}
	if decision.SelectedWorker != "w2" {
		t.Fatalf("selected worker = %q, want %q", decision.SelectedWorker, "w2")
	}
	if decision.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2", decision.AttemptCount)
	}
	if got := strings.Join(decision.AttemptedWorkers, ","); got != "w1,w2" {
		t.Fatalf("attempted workers = %v, want [w1 w2]", decision.AttemptedWorkers)
	}
	if got := strings.Join(decision.FailedWorkers, ","); got != "w1" {
		t.Fatalf("failed workers = %v, want [w1]", decision.FailedWorkers)
	}
	if decision.LastError != "boom" {
		t.Fatalf("last error = %q, want %q", decision.LastError, "boom")
	}
}

func TestSupervisorAgent_TimeoutEscalatesWhenConfigured(t *testing.T) {
	sess := &session.Session{ID: "s1", State: map[string]any{}}
	timeoutWorker := &scriptedAgent{
		name: "w1",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				<-ctx.Done()
				yield(nil, ctx.Err())
			}
		},
	}
	sup := &SupervisorAgent{
		AgentName:         "sup",
		Workers:           []kernel.Agent{timeoutWorker},
		WorkerTimeout:     10 * time.Millisecond,
		EscalateOnTimeout: true,
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			if len(workers) == 0 {
				return nil
			}
			return workers[0]
		},
	}

	var events []*session.Event
	for event, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 1 || events[0] == nil || !events[0].Actions.Escalate {
		t.Fatalf("expected one escalation event, got %#v", events)
	}
	decision := supervisorDecisionFromSession(t, sess, "patterns.supervisor.sup")
	if decision.Status != SupervisorStatusTimedOut {
		t.Fatalf("status = %q, want %q", decision.Status, SupervisorStatusTimedOut)
	}
	if !decision.Escalated || decision.EscalationReason != "timeout" {
		t.Fatalf("expected timeout escalation, got escalated=%v reason=%q", decision.Escalated, decision.EscalationReason)
	}
	if got := strings.Join(decision.TimedOutWorkers, ","); got != "w1" {
		t.Fatalf("timed out workers = %v, want [w1]", decision.TimedOutWorkers)
	}
}

func TestSupervisorAgent_TimeoutFailoverTracksHealth(t *testing.T) {
	sess := &session.Session{ID: "s1", State: map[string]any{}}
	timeoutWorker := &scriptedAgent{
		name: "w1",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				<-ctx.Done()
				yield(nil, ctx.Err())
			}
		},
	}
	okWorker := &stubAgent{name: "w2", events: []*session.Event{makeEvent("w2", "ok")}}
	sup := &SupervisorAgent{
		AgentName:       "sup",
		Workers:         []kernel.Agent{timeoutWorker, okWorker},
		WorkerTimeout:   10 * time.Millisecond,
		FailoverOnError: true,
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			if len(workers) == 0 {
				return nil
			}
			return workers[0]
		},
	}

	var authors []string
	for event, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event != nil {
			authors = append(authors, event.Author)
		}
	}

	if got := strings.Join(authors, ","); got != "w2" {
		t.Fatalf("authors = %v, want [w2]", authors)
	}
	decision := supervisorDecisionFromSession(t, sess, "patterns.supervisor.sup")
	if decision.Status != SupervisorStatusCompleted {
		t.Fatalf("status = %q, want %q", decision.Status, SupervisorStatusCompleted)
	}
	if got := strings.Join(decision.TimedOutWorkers, ","); got != "w1" {
		t.Fatalf("timed out workers = %v, want [w1]", decision.TimedOutWorkers)
	}
	timeoutHealth := decision.WorkerHealth["w1"]
	if timeoutHealth.TimeoutCount != 1 || timeoutHealth.LastStatus != SupervisorStatusTimedOut {
		t.Fatalf("timeout worker health = %+v, want timeout_count=1 last_status=timed_out", timeoutHealth)
	}
	okHealth := decision.WorkerHealth["w2"]
	if okHealth.SuccessCount != 1 || okHealth.LastStatus != SupervisorStatusCompleted {
		t.Fatalf("ok worker health = %+v, want success_count=1 last_status=completed", okHealth)
	}
}

func TestSupervisorAgent_SkipsSuppressedWorkerAcrossInvocations(t *testing.T) {
	now := time.Date(2026, 4, 11, 13, 30, 0, 0, time.UTC)
	sess := &session.Session{ID: "s1", State: map[string]any{}}
	failing := &stubAgent{name: "w1", err: fmt.Errorf("boom")}
	okWorker := &stubAgent{name: "w2", events: []*session.Event{makeEvent("w2", "ok")}}
	sup := &SupervisorAgent{
		AgentName:              "sup",
		Workers:                []kernel.Agent{failing, okWorker},
		FailoverOnError:        true,
		MaxConsecutiveFailures: 1,
		HealthCooldown:         time.Hour,
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			if len(workers) == 0 {
				return nil
			}
			return workers[0]
		},
		clock: func() time.Time { return now },
	}

	for _, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error on first run: %v", err)
		}
	}
	first := supervisorDecisionFromSession(t, sess, "patterns.supervisor.sup")
	if first.WorkerHealth["w1"].SuppressedUntil != now.Add(time.Hour) {
		t.Fatalf("suppressed_until = %v, want %v", first.WorkerHealth["w1"].SuppressedUntil, now.Add(time.Hour))
	}

	var authors []string
	for event, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error on second run: %v", err)
		}
		if event != nil {
			authors = append(authors, event.Author)
		}
	}

	if got := strings.Join(authors, ","); got != "w2" {
		t.Fatalf("authors = %v, want [w2]", authors)
	}
	second := supervisorDecisionFromSession(t, sess, "patterns.supervisor.sup")
	if got := strings.Join(second.AttemptedWorkers, ","); got != "w2" {
		t.Fatalf("attempted workers = %v, want [w2]", second.AttemptedWorkers)
	}
}

func TestSupervisorAgent_FiltersWorkersByBudget(t *testing.T) {
	sess := &session.Session{
		ID:    "s1",
		State: map[string]any{},
		Budget: session.Budget{
			MaxTokens:  100,
			MaxSteps:   10,
			UsedTokens: 95,
			UsedSteps:  8,
		},
	}
	expensive := &stubAgent{name: "expensive", events: []*session.Event{makeEvent("expensive", "too-expensive")}}
	cheap := &stubAgent{name: "cheap", events: []*session.Event{makeEvent("cheap", "ok")}}
	sup := &SupervisorAgent{
		AgentName: "sup",
		Workers:   []kernel.Agent{expensive, cheap},
		Router: func(_ *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
			if len(workers) == 0 {
				return nil
			}
			return workers[0]
		},
		WorkerBudgets: map[string]SupervisorWorkerBudget{
			"expensive": {MinRemainingTokens: 10, MinRemainingSteps: 3},
			"cheap":     {MinRemainingTokens: 1, MinRemainingSteps: 1},
		},
	}

	var authors []string
	for event, err := range sup.Run(testCtxWithSession(sess)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event != nil {
			authors = append(authors, event.Author)
		}
	}

	if got := strings.Join(authors, ","); got != "cheap" {
		t.Fatalf("authors = %v, want [cheap]", authors)
	}
	decision := supervisorDecisionFromSession(t, sess, "patterns.supervisor.sup")
	if decision.SelectedWorker != "cheap" {
		t.Fatalf("selected worker = %q, want cheap", decision.SelectedWorker)
	}
	if got := strings.Join(decision.BudgetFilteredWorkers, ","); got != "expensive" {
		t.Fatalf("budget filtered workers = %v, want [expensive]", decision.BudgetFilteredWorkers)
	}
}

func TestParallelAgent_IsolatesChildSessionsAndMaterializesMergedEvents(t *testing.T) {
	parent := &session.Session{ID: "parent", State: map[string]any{}}
	var (
		mu       sync.Mutex
		branches []*session.Session
	)
	a1 := &stubAgent{
		name: "a1",
		onRun: func(ctx *kernel.InvocationContext) {
			ctx.Session().SetState("worker", "a1")
			ctx.Session().AppendMessage(model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("a1")}})
			mu.Lock()
			branches = append(branches, ctx.Session())
			mu.Unlock()
		},
		events: []*session.Event{makeEvent("a1", "r1")},
	}
	a2 := &stubAgent{
		name: "a2",
		onRun: func(ctx *kernel.InvocationContext) {
			ctx.Session().SetState("worker", "a2")
			ctx.Session().AppendMessage(model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("a2")}})
			mu.Lock()
			branches = append(branches, ctx.Session())
			mu.Unlock()
		},
		events: []*session.Event{makeEvent("a2", "r2")},
	}

	par := &ParallelAgent{AgentName: "par", Agents: []kernel.Agent{a1, a2}}
	for _, err := range par.Run(testCtxWithSession(parent)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 child sessions, got %d", len(branches))
	}
	if branches[0] == parent || branches[1] == parent {
		t.Fatal("expected child sessions to be isolated from parent session")
	}
	if branches[0] == branches[1] {
		t.Fatal("expected parallel workers to receive distinct session clones")
	}
	if _, ok := parent.GetState("worker"); ok {
		t.Fatal("expected parent session state to remain untouched")
	}
	texts := sessionMessageTexts(parent)
	if len(texts) != 2 || texts[0] != "r1" || texts[1] != "r2" {
		t.Fatalf("expected merged events to materialize into parent session, got %v", texts)
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

func TestResearchAgent_PropagatesQueriesAndFindings(t *testing.T) {
	queryAgent := &stubAgent{
		name:   "query",
		events: []*session.Event{makeEvent("query", "query1\nquery2\nquery3")},
	}

	var (
		mu           sync.Mutex
		searchInputs []string
		synthInput   string
	)
	searchAgent := &scriptedAgent{
		name: "search",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				query := ""
				if ctx.UserContent() != nil {
					query = strings.TrimSpace(model.ContentPartsToPlainText(ctx.UserContent().ContentParts))
				}
				mu.Lock()
				searchInputs = append(searchInputs, query)
				mu.Unlock()
				yield(makeEvent("search", "finding for "+query), nil)
			}
		},
	}
	synthAgent := &scriptedAgent{
		name: "synth",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				if ctx.UserContent() != nil {
					synthInput = model.ContentPartsToPlainText(ctx.UserContent().ContentParts)
				}
				yield(makeEvent("synth", "answer"), nil)
			}
		},
	}

	research := NewResearchAgent(ResearchConfig{
		Name:                "research",
		QueryAgent:          queryAgent,
		SearchAgent:         searchAgent,
		SynthesisAgent:      synthAgent,
		MaxIterations:       1,
		MaxParallelSearches: 2,
	})

	for _, err := range research.Run(testCtx()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	sort.Strings(searchInputs)
	if got := strings.Join(searchInputs, ","); got != "query1,query2,query3" {
		t.Fatalf("search inputs = %v, want [query1 query2 query3]", searchInputs)
	}
	for _, fragment := range []string{"query1", "query2", "query3", "finding for query1", "finding for query2", "finding for query3"} {
		if !strings.Contains(synthInput, fragment) {
			t.Fatalf("expected synthesis input to contain %q, got %q", fragment, synthInput)
		}
	}
}

func TestResearchAgent_MaterializesEventsIntoParentSession(t *testing.T) {
	parent := &session.Session{ID: "parent", State: map[string]any{}}
	queryAgent := &stubAgent{
		name:   "query",
		events: []*session.Event{makeEvent("query", "query1\nquery2")},
	}
	searchAgent := &scriptedAgent{
		name: "search",
		run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				query := strings.TrimSpace(model.ContentPartsToPlainText(ctx.UserContent().ContentParts))
				event := makeEvent("search", "finding for "+query)
				event.Actions.StateDelta = map[string]any{"search." + query: "done"}
				yield(event, nil)
			}
		},
	}
	synthAgent := &scriptedAgent{
		name: "synth",
		run: func(_ *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := makeEvent("synth", "answer")
				event.Actions.StateDelta = map[string]any{"research.answer": "ready"}
				yield(event, nil)
			}
		},
	}

	research := NewResearchAgent(ResearchConfig{
		Name:                "research",
		QueryAgent:          queryAgent,
		SearchAgent:         searchAgent,
		SynthesisAgent:      synthAgent,
		MaxIterations:       1,
		MaxParallelSearches: 2,
	})

	for _, err := range research.Run(testCtxWithSession(parent)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	texts := sessionMessageTexts(parent)
	if len(texts) != 4 || texts[0] != "query1\nquery2" || texts[1] != "finding for query1" || texts[2] != "finding for query2" || texts[3] != "answer" {
		t.Fatalf("parent session messages = %v, want [query1\\nquery2 finding for query1 finding for query2 answer]", texts)
	}
	expectedState := map[string]string{
		"search.query1":   "done",
		"search.query2":   "done",
		"research.answer": "ready",
	}
	for key, want := range expectedState {
		got, ok := parent.GetState(key)
		if !ok || got != want {
			t.Fatalf("parent state %q = %v, want %q", key, got, want)
		}
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

// ─── Accessor tests ─────────────────────────────────────────────────────────

func TestSequentialAgent_Accessors(t *testing.T) {
child1 := &stubAgent{name: "c1"}
child2 := &stubAgent{name: "c2"}
a := &SequentialAgent{AgentName: "seq", Desc: "sequential agent", Agents: []kernel.Agent{child1, child2}}
if a.Name() != "seq" {
t.Fatalf("Name = %q, want seq", a.Name())
}
if a.Description() != "sequential agent" {
t.Fatalf("Description = %q", a.Description())
}
if len(a.SubAgents()) != 2 {
t.Fatalf("SubAgents len = %d, want 2", len(a.SubAgents()))
}
}

func TestParallelAgent_Accessors(t *testing.T) {
child := &stubAgent{name: "p1"}
a := &ParallelAgent{AgentName: "par", Desc: "parallel agent", Agents: []kernel.Agent{child}}
if a.Name() != "par" {
t.Fatalf("Name = %q, want par", a.Name())
}
if a.Description() != "parallel agent" {
t.Fatalf("Description = %q", a.Description())
}
if len(a.SubAgents()) != 1 {
t.Fatalf("SubAgents len = %d, want 1", len(a.SubAgents()))
}
}

func TestLoopAgent_Accessors(t *testing.T) {
child := &stubAgent{name: "inner"}
a := &LoopAgent{AgentName: "loop", Desc: "loop agent", Agent: child}
if a.Name() != "loop" {
t.Fatalf("Name = %q, want loop", a.Name())
}
if a.Description() != "loop agent" {
t.Fatalf("Description = %q", a.Description())
}
subs := a.SubAgents()
if len(subs) != 1 || subs[0].Name() != "inner" {
t.Fatalf("SubAgents = %v, want [inner]", subs)
}
}

func TestLoopAgent_SubAgentsNilInner(t *testing.T) {
a := &LoopAgent{AgentName: "loop"}
if a.SubAgents() != nil {
t.Fatal("SubAgents should be nil when Agent is nil")
}
}

func TestSupervisorAgent_Accessors(t *testing.T) {
w1 := &stubAgent{name: "worker1"}
w2 := &stubAgent{name: "worker2"}
a := &SupervisorAgent{AgentName: "sup", Desc: "supervisor", Workers: []kernel.Agent{w1, w2}}
if a.Name() != "sup" {
t.Fatalf("Name = %q, want sup", a.Name())
}
if a.Description() != "supervisor" {
t.Fatalf("Description = %q", a.Description())
}
if len(a.SubAgents()) != 2 {
t.Fatalf("SubAgents len = %d, want 2", len(a.SubAgents()))
}
}

func TestResearchAgent_Accessors(t *testing.T) {
q := &stubAgent{name: "query"}
s := &stubAgent{name: "search"}
synth := &stubAgent{name: "synth"}
a := NewResearchAgent(ResearchConfig{
Name:           "research",
Description:    "research agent",
QueryAgent:     q,
SearchAgent:    s,
SynthesisAgent: synth,
})
if a.Name() != "research" {
t.Fatalf("Name = %q, want research", a.Name())
}
if a.Description() != "research agent" {
t.Fatalf("Description = %q", a.Description())
}
subs := a.SubAgents()
if len(subs) != 3 {
t.Fatalf("SubAgents len = %d, want 3", len(subs))
}
}

func TestRoundRobinRouter_CyclesWorkers(t *testing.T) {
	ctx := testCtx()
	workers := []kernel.Agent{&stubAgent{name: "w0"}, &stubAgent{name: "w1"}, &stubAgent{name: "w2"}}
	router := RoundRobinRouter("rr-state")

	selections := make([]string, 6)
	for i := range selections {
		w := router(ctx, workers)
		if w == nil {
			t.Fatalf("iteration %d: got nil worker", i)
		}
		selections[i] = w.Name()
	}
	// Should cycle: w0,w1,w2,w0,w1,w2
	expected := []string{"w0", "w1", "w2", "w0", "w1", "w2"}
	for i, e := range expected {
		if selections[i] != e {
			t.Fatalf("iteration %d: got %q, want %q", i, selections[i], e)
		}
	}
}

func TestFirstMatchRouter_SelectsMatch(t *testing.T) {
	ctx := testCtx()
	workers := []kernel.Agent{&stubAgent{name: "alpha"}, &stubAgent{name: "beta"}, &stubAgent{name: "gamma"}}

	router := FirstMatchRouter(func(_ *kernel.InvocationContext, w kernel.Agent) bool {
		return w.Name() == "beta"
	})
	got := router(ctx, workers)
	if got == nil || got.Name() != "beta" {
		t.Fatalf("expected beta, got %v", got)
	}
}

func TestFirstMatchRouter_NoMatch(t *testing.T) {
	ctx := testCtx()
	workers := []kernel.Agent{&stubAgent{name: "alpha"}}

	router := FirstMatchRouter(func(_ *kernel.InvocationContext, w kernel.Agent) bool {
		return false
	})
	if got := router(ctx, workers); got != nil {
		t.Fatalf("expected nil for no match, got %v", got)
	}
}
