package kernel

import (
	"context"

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

	agent       Agent
	session     *session.Session
	userContent *model.Message

	io       io.UserIO
	observer observe.Observer

	ended bool
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

// IO returns the user interaction port.
func (c *InvocationContext) IO() io.UserIO { return c.io }

// Observer returns the observability observer.
func (c *InvocationContext) Observer() observe.Observer { return c.observer }

// EndInvocation signals that the current invocation should stop.
func (c *InvocationContext) EndInvocation() { c.ended = true }

// Ended returns whether EndInvocation was called.
func (c *InvocationContext) Ended() bool { return c.ended }

// WithAgent returns a new InvocationContext for a different agent, preserving other fields.
func (c *InvocationContext) WithAgent(agent Agent) *InvocationContext {
	cp := *c
	cp.agent = agent
	return &cp
}

// WithBranch returns a new InvocationContext with an updated branch path.
func (c *InvocationContext) WithBranch(branch string) *InvocationContext {
	cp := *c
	cp.branch = branch
	return &cp
}

// WithContext returns a new InvocationContext with a different base context.
func (c *InvocationContext) WithContext(ctx context.Context) *InvocationContext {
	cp := *c
	cp.Context = ctx
	return &cp
}

// WithIO returns a new InvocationContext with a different UserIO.
func (c *InvocationContext) WithIO(userIO io.UserIO) *InvocationContext {
	cp := *c
	cp.io = userIO
	return &cp
}
