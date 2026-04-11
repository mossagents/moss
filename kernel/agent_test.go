package kernel_test

import (
	"context"
	"iter"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

// --- helpers ---

func textEvent(author, text string) *session.Event {
	return &session.Event{
		ID:     "test-evt",
		Author: author,
		Content: &model.Message{
			Role:         model.RoleAssistant,
			ContentParts: []model.ContentPart{model.TextPart(text)},
		},
		Timestamp: time.Now().UTC(),
	}
}

func echoAgent(name string) *kernel.CustomAgent {
	return kernel.NewCustomAgent(kernel.CustomAgentConfig{
		Name: name,
		Run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(textEvent(name, "hello from "+name), nil)
			}
		},
	})
}

func collectEvents(seq iter.Seq2[*session.Event, error]) ([]*session.Event, error) {
	var events []*session.Event
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}

func eventText(e *session.Event) string {
	if e == nil || e.Content == nil {
		return ""
	}
	return model.ContentPartsToPlainText(e.Content.ContentParts)
}

// --- Agent interface ---

func TestCustomAgent_Name(t *testing.T) {
	a := echoAgent("test-agent")
	if a.Name() != "test-agent" {
		t.Fatalf("expected name 'test-agent', got %q", a.Name())
	}
}

func TestCustomAgent_Run(t *testing.T) {
	a := echoAgent("greeter")
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   a,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(a.Run(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if got := eventText(events[0]); got != "hello from greeter" {
		t.Fatalf("expected 'hello from greeter', got %q", got)
	}
	if events[0].Author != "greeter" {
		t.Fatalf("expected author 'greeter', got %q", events[0].Author)
	}
}

func TestCustomAgent_Description(t *testing.T) {
	a := kernel.NewCustomAgent(kernel.CustomAgentConfig{
		Name:        "desc-agent",
		Description: "a test agent",
		Run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if a.Description() != "a test agent" {
		t.Fatalf("expected description 'a test agent', got %q", a.Description())
	}
}

// --- SequentialAgent ---

func TestSequentialAgent_RunsInOrder(t *testing.T) {
	seq := kernel.NewSequentialAgent("pipeline", "runs A then B",
		echoAgent("A"),
		echoAgent("B"),
	)
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   seq,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(seq.Run(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if got := eventText(events[0]); got != "hello from A" {
		t.Fatalf("expected 'hello from A', got %q", got)
	}
	if got := eventText(events[1]); got != "hello from B" {
		t.Fatalf("expected 'hello from B', got %q", got)
	}
}

func TestSequentialAgent_StopsOnEscalate(t *testing.T) {
	escalating := kernel.NewCustomAgent(kernel.CustomAgentConfig{
		Name: "escalator",
		Run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				e := textEvent("escalator", "escalating")
				e.Actions.Escalate = true
				yield(e, nil)
			}
		},
	})
	seq := kernel.NewSequentialAgent("pipe", "",
		escalating,
		echoAgent("should-not-run"),
	)
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   seq,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(seq.Run(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event (escalate stops further agents), got %d", len(events))
	}
}

func TestSequentialAgent_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seq := kernel.NewSequentialAgent("pipe", "", echoAgent("A"))
	invCtx := kernel.NewInvocationContext(ctx, kernel.InvocationContextParams{
		Agent:   seq,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(seq.Run(invCtx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events with cancelled context, got %d", len(events))
	}
}

// --- ParallelAgent ---

func TestParallelAgent_RunsConcurrently(t *testing.T) {
	var order atomic.Int32
	slowAgent := func(name string, delay time.Duration) *kernel.CustomAgent {
		return kernel.NewCustomAgent(kernel.CustomAgentConfig{
			Name: name,
			Run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
				return func(yield func(*session.Event, error) bool) {
					time.Sleep(delay)
					idx := order.Add(1)
					e := textEvent(name, name)
					e.ID = string(rune('0' + idx))
					yield(e, nil)
				}
			},
		})
	}

	par := kernel.NewParallelAgent("parallel", "concurrent",
		slowAgent("slow", 50*time.Millisecond),
		slowAgent("fast", 10*time.Millisecond),
	)
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   par,
		Session: &session.Session{ID: "s1"},
	})

	start := time.Now()
	events, err := collectEvents(par.Run(ctx))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Events are yielded in agent order (slow first, fast second), not completion order.
	if events[0].Author != "slow" {
		t.Fatalf("expected first event from 'slow', got %q", events[0].Author)
	}
	// Both should complete within ~60ms if truly parallel (not 60+ms).
	if elapsed > 200*time.Millisecond {
		t.Fatalf("parallel agent took too long: %v (expected < 200ms)", elapsed)
	}
}

// --- LoopAgent ---

func TestLoopAgent_RepeatsUntilStop(t *testing.T) {
	var count int
	counter := kernel.NewCustomAgent(kernel.CustomAgentConfig{
		Name: "counter",
		Run: func(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				count++
				e := textEvent("counter", "done")
				if count >= 3 {
					e.Content.ContentParts[0].Text = "stop"
				}
				yield(e, nil)
			}
		},
	})

	loopAg := kernel.NewLoopAgent(kernel.LoopAgentConfig{
		Name:     "looper",
		SubAgent: counter,
		MaxIter:  10,
		ShouldStop: func(e *session.Event) bool {
			return eventText(e) == "stop"
		},
	})
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   loopAg,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(loopAg.Run(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events (loop stops at count=3), got %d", len(events))
	}
}

func TestLoopAgent_RespectsMaxIter(t *testing.T) {
	loopAg := kernel.NewLoopAgent(kernel.LoopAgentConfig{
		Name:     "bounded",
		SubAgent: echoAgent("echo"),
		MaxIter:  5,
	})
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   loopAg,
		Session: &session.Session{ID: "s1"},
	})

	events, err := collectEvents(loopAg.Run(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events (one per iteration), got %d", len(events))
	}
}

// --- FindAgentInTree ---

func TestFindAgentInTree(t *testing.T) {
	leaf := echoAgent("leaf")
	mid := kernel.NewSequentialAgent("mid", "", leaf)
	root := kernel.NewSequentialAgent("root", "", mid, echoAgent("other"))

	found := kernel.FindAgentInTree(root, "leaf")
	if found == nil {
		t.Fatal("expected to find 'leaf', got nil")
	}
	if found.Name() != "leaf" {
		t.Fatalf("expected 'leaf', got %q", found.Name())
	}

	notFound := kernel.FindAgentInTree(root, "nonexistent")
	if notFound != nil {
		t.Fatalf("expected nil for nonexistent agent, got %q", notFound.Name())
	}
}

// --- InvocationContext ---

func TestInvocationContext_Accessors(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	agent := echoAgent("test")
	msg := &model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}

	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		InvocationID: "inv-1",
		Branch:       "root.test",
		RunID:        "run-1",
		Agent:        agent,
		Session:      sess,
		UserContent:  msg,
	})

	if ctx.InvocationID() != "inv-1" {
		t.Fatalf("expected invocation ID 'inv-1', got %q", ctx.InvocationID())
	}
	if ctx.Branch() != "root.test" {
		t.Fatalf("expected branch 'root.test', got %q", ctx.Branch())
	}
	if ctx.RunID() != "run-1" {
		t.Fatalf("expected run ID 'run-1', got %q", ctx.RunID())
	}
	if ctx.Agent().Name() != "test" {
		t.Fatalf("expected agent 'test', got %q", ctx.Agent().Name())
	}
	if ctx.Session().ID != "s1" {
		t.Fatalf("expected session 's1', got %q", ctx.Session().ID)
	}
}

func TestInvocationContext_WithAgent(t *testing.T) {
	original := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   echoAgent("original"),
		Session: &session.Session{ID: "s1"},
		RunID:   "run-1",
	})

	newAgent := echoAgent("new")
	derived := original.WithAgent(newAgent)

	if derived.Agent().Name() != "new" {
		t.Fatalf("expected derived agent 'new', got %q", derived.Agent().Name())
	}
	if original.Agent().Name() != "original" {
		t.Fatalf("original should be unchanged, got %q", original.Agent().Name())
	}
	// RunID should be preserved.
	if derived.RunID() != "run-1" {
		t.Fatalf("expected RunID preserved, got %q", derived.RunID())
	}
}

func TestInvocationContext_EndInvocation(t *testing.T) {
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   echoAgent("test"),
		Session: &session.Session{ID: "s1"},
	})

	if ctx.Ended() {
		t.Fatal("expected Ended() to be false initially")
	}
	ctx.EndInvocation()
	if !ctx.Ended() {
		t.Fatal("expected Ended() to be true after EndInvocation()")
	}
}

func TestInvocationContext_AutoGeneratesID(t *testing.T) {
	ctx := kernel.NewInvocationContext(context.Background(), kernel.InvocationContextParams{
		Agent:   echoAgent("test"),
		Session: &session.Session{ID: "s1"},
	})

	if !strings.HasPrefix(ctx.InvocationID(), "inv-") {
		t.Fatalf("expected auto-generated ID starting with 'inv-', got %q", ctx.InvocationID())
	}
}

// --- Runner ---

func TestRunner_RequiresAgent(t *testing.T) {
	_, err := kernel.NewRunner(kernel.RunnerConfig{})
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestRunner_RunYieldsEvents(t *testing.T) {
	agent := echoAgent("runner-test")
	r, err := kernel.NewRunner(kernel.RunnerConfig{Agent: agent})
	if err != nil {
		t.Fatalf("unexpected error creating runner: %v", err)
	}

	sess := &session.Session{ID: "s1"}
	events, err := collectEvents(r.Run(context.Background(), sess, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if got := eventText(events[0]); got != "hello from runner-test" {
		t.Fatalf("expected 'hello from runner-test', got %q", got)
	}
}

func TestRunner_AppendsUserMessage(t *testing.T) {
	agent := echoAgent("test")
	r, _ := kernel.NewRunner(kernel.RunnerConfig{Agent: agent})
	sess := &session.Session{ID: "s1"}
	userMsg := &model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("user input")},
	}

	_, _ = collectEvents(r.Run(context.Background(), sess, userMsg))

	msgs := sess.CopyMessages()
	if len(msgs) == 0 {
		t.Fatal("expected user message to be appended to session")
	}
	if msgs[0].Role != model.RoleUser {
		t.Fatalf("expected first message role 'user', got %q", msgs[0].Role)
	}
}
