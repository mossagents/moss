package kernel

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync/atomic"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

// InvocationContext carries all context needed for an agent invocation.
// It embeds context.Context for cancellation and deadline propagation.
type InvocationContext struct {
	context.Context

	invocationID string
	branch       string // agent hierarchy path: "root.sub1.sub2"
	runID        string

	agent        Agent
	session      *session.Session
	userContent  *model.Message
	resultWriter func(*session.LifecycleResult)

	io       io.UserIO
	observer observe.Observer

	ended atomic.Bool
}

// InvocationContextParams contains parameters for creating an InvocationContext.
type InvocationContextParams struct {
	InvocationID string
	Branch       string
	RunID        string
	Agent        Agent
	Session      *session.Session
	UserContent  *model.Message
	IO           io.UserIO
	Observer     observe.Observer
	resultWriter func(*session.LifecycleResult)
}

// ChildRunConfig controls how a custom agent or orchestration primitive invokes
// a child agent.
type ChildRunConfig struct {
	// Branch overrides the derived branch path for the child invocation.
	Branch string
	// UserContent replaces the inherited user content for the child invocation.
	UserContent *model.Message
	// PrepareSession mutates the branch-local child session before the agent runs.
	PrepareSession func(*session.Session)
	// DisableMaterialization skips committing yielded child events back into the
	// parent session. This is primarily useful for parallel fan-out branches that
	// aggregate results before committing them.
	DisableMaterialization bool
}

// NewInvocationContext creates a new InvocationContext from the given parameters.
func NewInvocationContext(ctx context.Context, params InvocationContextParams) *InvocationContext {
	if params.InvocationID == "" {
		params.InvocationID = generateInvocationID()
	}
	if params.Observer == nil {
		params.Observer = observe.NoOpObserver{}
	}
	return &InvocationContext{
		Context:      ctx,
		invocationID: params.InvocationID,
		branch:       params.Branch,
		runID:        params.RunID,
		agent:        params.Agent,
		session:      params.Session,
		userContent:  params.UserContent,
		resultWriter: params.resultWriter,
		io:           params.IO,
		observer:     params.Observer,
	}
}

// InvocationID returns the unique identifier for this invocation.
func (c *InvocationContext) InvocationID() string { return c.invocationID }

// Branch returns the agent hierarchy path (e.g., "root.sub1.sub2").
func (c *InvocationContext) Branch() string { return c.branch }

// RunID returns the run identifier.
func (c *InvocationContext) RunID() string { return c.runID }

// Agent returns the agent being invoked.
func (c *InvocationContext) Agent() Agent { return c.agent }

// Session returns the session for this invocation.
func (c *InvocationContext) Session() *session.Session { return c.session }

// UserContent returns the user's input message that triggered this invocation.
func (c *InvocationContext) UserContent() *model.Message { return c.userContent }

func (c *InvocationContext) setLifecycleResult(result *session.LifecycleResult) {
	if c == nil || c.resultWriter == nil {
		return
	}
	c.resultWriter(result)
}

// IO returns the user interaction port.
func (c *InvocationContext) IO() io.UserIO { return c.io }

// Observer returns the observability observer.
func (c *InvocationContext) Observer() observe.Observer { return c.observer }

// EndInvocation signals that the current invocation should stop.
func (c *InvocationContext) EndInvocation() { c.ended.Store(true) }

// Ended returns whether EndInvocation was called.
func (c *InvocationContext) Ended() bool { return c.ended.Load() }

// shallowCopy returns a new InvocationContext that shares all scalar/pointer
// fields with the receiver but has its own ended flag (reset to false).
// atomic.Bool must not be copied, so we rebuild the struct explicitly.
func (c *InvocationContext) shallowCopy() *InvocationContext {
	return &InvocationContext{
		Context:      c.Context,
		invocationID: c.invocationID,
		branch:       c.branch,
		runID:        c.runID,
		agent:        c.agent,
		session:      c.session,
		userContent:  c.userContent,
		resultWriter: c.resultWriter,
		io:           c.io,
		observer:     c.observer,
		// ended is intentionally zero-valued (false)
	}
}

// WithAgent returns a new InvocationContext for a different agent, preserving other fields.
func (c *InvocationContext) WithAgent(agent Agent) *InvocationContext {
	cp := c.shallowCopy()
	cp.agent = agent
	return cp
}

// WithBranch returns a new InvocationContext with an updated branch path.
func (c *InvocationContext) WithBranch(branch string) *InvocationContext {
	cp := c.shallowCopy()
	cp.branch = branch
	return cp
}

// WithSession returns a new InvocationContext with a different session.
func (c *InvocationContext) WithSession(sess *session.Session) *InvocationContext {
	cp := c.shallowCopy()
	cp.session = sess
	return cp
}

// WithUserContent returns a new InvocationContext with different input content.
func (c *InvocationContext) WithUserContent(msg *model.Message) *InvocationContext {
	cp := c.shallowCopy()
	cp.userContent = msg
	return cp
}

// WithContext returns a new InvocationContext with a different base context.
func (c *InvocationContext) WithContext(ctx context.Context) *InvocationContext {
	cp := c.shallowCopy()
	cp.Context = ctx
	return cp
}

// WithIO returns a new InvocationContext with a different UserIO.
func (c *InvocationContext) WithIO(userIO io.UserIO) *InvocationContext {
	cp := c.shallowCopy()
	cp.io = userIO
	return cp
}

// maxForkDepth is the maximum allowed nesting depth for child agent invocations.
// Deeper nesting causes exponential resource consumption.
const maxForkDepth = 5

// maxActiveAgents limits the number of concurrently running child agents across
// the current process. This prevents runaway fan-out from exhausting memory,
// model quota, or workspace resources.
const maxActiveAgents = 16

var activeChildAgentCount atomic.Int32

// forkDepth calculates the current fork nesting depth from a branch path.
func forkDepth(branch string) int {
	if branch == "" {
		return 0
	}
	return 1 + strings.Count(branch, ".")
}

// RunChild executes a child agent on a branch-local session clone. By default,
// yielded non-partial events are materialized back into the parent session
// before they are yielded to the caller. Events retain structured
// materialization-domain markers, so nested child runs no longer need to reset
// a global boolean to allow outer domains to commit them.
func (c *InvocationContext) RunChild(agent Agent, cfg ChildRunConfig) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		if c == nil {
			yield(nil, fmt.Errorf("invocation context is nil"))
			return
		}
		if agent == nil {
			yield(nil, fmt.Errorf("child agent is nil"))
			return
		}
		if forkDepth(c.Branch()) >= maxForkDepth {
			yield(nil, fmt.Errorf("fork depth limit reached (%d): cannot spawn child agent %q", maxForkDepth, agent.Name()))
			return
		}
		if active := activeChildAgentCount.Add(1); active > maxActiveAgents {
			activeChildAgentCount.Add(-1)
			yield(nil, fmt.Errorf("active child agent limit reached (%d): cannot spawn child agent %q", maxActiveAgents, agent.Name()))
			return
		}
		defer activeChildAgentCount.Add(-1)

		childCtx := c.WithAgent(agent).WithBranch(childBranch(c.Branch(), agent.Name(), cfg.Branch))
		if cfg.UserContent != nil {
			childCtx = childCtx.WithUserContent(cfg.UserContent)
		}
		if childSession := c.Session().Clone(); childSession != nil {
			if cfg.PrepareSession != nil {
				cfg.PrepareSession(childSession)
			}
			if cfg.UserContent != nil {
				childSession.AppendMessage(session.CloneMessage(*cfg.UserContent))
			}
			childCtx = childCtx.WithSession(childSession)
		}

		for event, err := range agent.Run(childCtx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if !cfg.DisableMaterialization {
				session.MaterializeEvent(c.Session(), event)
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

func childBranch(parent, agentName, override string) string {
	if branch := strings.TrimSpace(override); branch != "" {
		return branch
	}
	if parent = strings.TrimSpace(parent); parent == "" {
		return agentName
	}
	if agentName = strings.TrimSpace(agentName); agentName == "" {
		return parent
	}
	return parent + "." + agentName
}
