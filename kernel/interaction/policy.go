package interaction

import (
	"encoding/json"
	"github.com/mossagents/moss/kernel/tool"
)

// PolicyDecision 表示权限决策。
type PolicyDecision string

const (
	PolicyAllow           PolicyDecision = "allow"
	PolicyDeny            PolicyDecision = "deny"
	PolicyRequireApproval PolicyDecision = "require_approval"
)

// EnforcementMode 表示策略如何生效。
type EnforcementMode string

const (
	EnforcementHardBlock       EnforcementMode = "hard_block"
	EnforcementRequireApproval EnforcementMode = "require_approval"
	EnforcementSoftLimit       EnforcementMode = "soft_limit"
)

// PolicyReason 描述策略决策的结构化原因。
type PolicyReason struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// PolicyContext 是一次工具调用进入策略评估时的结构化上下文。
type PolicyContext struct {
	SessionID    string          `json:"session_id,omitempty"`
	SessionState map[string]any  `json:"-"`
	Identity     *Identity       `json:"identity,omitempty"`
	Tool         tool.ToolSpec   `json:"tool"`
	Input        json.RawMessage `json:"input,omitempty"`
}

// PolicyResult 是一次策略评估的结构化输出。
type PolicyResult struct {
	Decision    PolicyDecision  `json:"decision"`
	Enforcement EnforcementMode `json:"enforcement,omitempty"`
	Reason      PolicyReason    `json:"reason,omitempty"`
	Meta        map[string]any  `json:"meta,omitempty"`
}
