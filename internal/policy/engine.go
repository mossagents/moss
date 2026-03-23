package policy

import (
	"github.com/mossagi/moss/internal/workspace"
)

type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionDeny    Decision = "deny"
	DecisionApprove Decision = "require_approval"
)

type CheckRequest struct {
	RunID          string
	AgentName      string
	ToolName       string
	Capabilities   []string
	WorkspaceTrust workspace.TrustLevel
	AllowedCaps    []string
}

type Engine struct{}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) Check(req CheckRequest) Decision {
	// run_command and write_file always require approval
	if req.ToolName == "run_command" || req.ToolName == "write_file" {
		return DecisionApprove
	}

	// restricted workspace: high risk tools denied
	if req.WorkspaceTrust == workspace.TrustLevelRestricted {
		for _, cap := range req.Capabilities {
			if cap == "execute" || cap == "write" {
				return DecisionDeny
			}
		}
	}

	// agent must have required capability
	if len(req.AllowedCaps) > 0 {
		for _, required := range req.Capabilities {
			allowed := false
			for _, cap := range req.AllowedCaps {
				if cap == required {
					allowed = true
					break
				}
			}
			if !allowed {
				return DecisionDeny
			}
		}
	}

	return DecisionAllow
}
