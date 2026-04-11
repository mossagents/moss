package kernel

import (
	"context"
	"fmt"
	"iter"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

// RunnerConfig configures a Runner.
type RunnerConfig struct {
	// Agent is the root agent to execute (required).
	Agent Agent
	// IO is the user interaction port (optional).
	IO io.UserIO
	// Observer for runtime observability (optional, defaults to NoOp).
	Observer observe.Observer
}

// Runner orchestrates agent execution, managing session lifecycle and event flow.
type Runner struct {
	agent    Agent
	io       io.UserIO
	observer observe.Observer
}

// NewRunner creates a Runner with the given configuration.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("runner: agent is required")
	}
	if cfg.Observer == nil {
		cfg.Observer = observe.NoOpObserver{}
	}
	return &Runner{
		agent:    cfg.Agent,
		io:       cfg.IO,
		observer: cfg.Observer,
	}, nil
}

// Run executes the root agent with the given session and user input.
// Returns an iterator that yields events produced during execution. Generic
// yielded events are materialized back into sess unless they have already been
// committed in the same materialization domain by an inner execution layer.
func (r *Runner) Run(ctx context.Context, sess *session.Session, input *model.Message) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Append user message to session.
		if input != nil {
			sess.AppendMessage(session.CloneMessage(*input))
		}

		// Ensure IO is goroutine-safe.
		userIO := r.io
		if userIO != nil {
			if _, ok := userIO.(*io.SyncIO); !ok {
				userIO = io.NewSyncIO(userIO)
			}
		}

		// Determine which agent to run (supports agent transfer via session history).
		agentToRun := r.findAgentToRun(sess)

		// Create invocation context.
		invCtx := NewInvocationContext(ctx, InvocationContextParams{
			Branch:      agentToRun.Name(),
			Agent:       agentToRun,
			Session:     sess,
			UserContent: input,
			IO:          userIO,
			Observer:    r.observer,
		})
		streamAgentEvents(r.agent, invCtx, yield)
	}
}

// findAgentToRun determines which agent should handle the current invocation.
// It checks the session's last event author for agent transfer, falling back to the root agent.
func (r *Runner) findAgentToRun(sess *session.Session) Agent {
	// Check if the last non-user event was from a sub-agent (agent transfer scenario).
	messages := sess.CopyMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == model.RoleUser {
			continue
		}
		// For now, always use root agent. Agent transfer routing
		// will be enhanced once the Event-based session (Phase 2) is in place.
		break
	}
	return r.agent
}

// RootAgent returns the runner's root agent.
func (r *Runner) RootAgent() Agent { return r.agent }
