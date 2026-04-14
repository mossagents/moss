package kernel

import (
	kerr "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

// RunAgentRequest configures one canonical agent execution.
type RunAgentRequest struct {
	Session     *session.Session
	Agent       Agent
	UserContent *model.Message
	IO          io.UserIO
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
