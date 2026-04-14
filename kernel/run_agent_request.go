package kernel

import (
	"fmt"

	kerr "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// RunAgentRequest configures one canonical agent execution.
type RunAgentRequest struct {
	Session     *session.Session
	Agent       Agent
	UserContent *model.Message
	IO          io.UserIO
	Tools       tool.Registry
	OnResult    func(*session.LifecycleResult)
}

func (k *Kernel) normalizeRunAgentRequest(req RunAgentRequest) (RunAgentRequest, error) {
	if req.Session == nil {
		return RunAgentRequest{}, kerr.New(kerr.ErrValidation, "run agent requires a session")
	}
	if req.Agent == nil {
		return RunAgentRequest{}, kerr.New(kerr.ErrValidation, "run agent requires an agent")
	}
	req.IO = normalizeRunIO(req.IO, k.io)
	if req.Tools != nil {
		agent, err := rebindAgentTools(req.Agent, req.Tools)
		if err != nil {
			return RunAgentRequest{}, err
		}
		req.Agent = agent
	}
	return req, nil
}

func normalizeRunIO(override, fallback io.UserIO) io.UserIO {
	userIO := override
	if userIO == nil {
		userIO = fallback
	}
	if userIO == nil {
		return nil
	}
	if _, ok := userIO.(*io.SyncIO); ok {
		return userIO
	}
	return io.NewSyncIO(userIO)
}

func rebindAgentTools(agent Agent, tools tool.Registry) (Agent, error) {
	llmAgent, ok := agent.(*LLMAgent)
	if !ok {
		return nil, kerr.New(
			kerr.ErrValidation,
			fmt.Sprintf("agent %q does not support request-scoped tool override", agent.Name()),
		)
	}
	return llmAgent.withTools(tools), nil
}
