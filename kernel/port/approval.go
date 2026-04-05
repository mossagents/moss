package port

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ApprovalKind 表示审批对象类型。
type ApprovalKind string

const (
	ApprovalKindTool ApprovalKind = "tool"
)

type ApprovalCategory string

const (
	ApprovalCategoryTool    ApprovalCategory = "tool"
	ApprovalCategoryCommand ApprovalCategory = "command"
	ApprovalCategoryHTTP    ApprovalCategory = "http"
)

type ApprovalDecisionType string

const (
	ApprovalDecisionApprove         ApprovalDecisionType = "approve"
	ApprovalDecisionApproveSession  ApprovalDecisionType = "approve_for_session"
	ApprovalDecisionPolicyAmendment ApprovalDecisionType = "policy_amendment"
	ApprovalDecisionGrantPermission ApprovalDecisionType = "grant_permission"
	ApprovalDecisionDeny            ApprovalDecisionType = "deny"
)

type PermissionProfile struct {
	CommandPaths   []string                  `json:"command_paths,omitempty"`
	HTTPHosts      []string                  `json:"http_hosts,omitempty"`
	CommandNetwork *CommandNetworkPermission `json:"command_network,omitempty"`
}

type CommandNetworkPermission struct {
	Enabled    bool     `json:"enabled,omitempty"`
	AllowHosts []string `json:"allow_hosts,omitempty"`
}

type ExecPolicyAmendment struct {
	CommandRule *ExecPolicyCommandRule `json:"command_rule,omitempty"`
	HTTPRule    *ExecPolicyHTTPRule    `json:"http_rule,omitempty"`
}

type ExecPolicyCommandRule struct {
	Name  string `json:"name,omitempty"`
	Match string `json:"match,omitempty"`
}

type ExecPolicyHTTPRule struct {
	Name    string   `json:"name,omitempty"`
	Match   string   `json:"match,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

// ApprovalRequest 描述一次结构化审批请求。
type ApprovalRequest struct {
	ID                   string               `json:"id"`
	Kind                 ApprovalKind         `json:"kind"`
	Category             ApprovalCategory     `json:"category,omitempty"`
	SessionID            string               `json:"session_id,omitempty"`
	ToolName             string               `json:"tool_name,omitempty"`
	Risk                 string               `json:"risk,omitempty"`
	Prompt               string               `json:"prompt"`
	Reason               string               `json:"reason,omitempty"`
	ReasonCode           string               `json:"reason_code,omitempty"`
	Enforcement          EnforcementMode      `json:"enforcement,omitempty"`
	Input                json.RawMessage      `json:"input,omitempty"`
	ActionLabel          string               `json:"action_label,omitempty"`
	ActionValue          string               `json:"action_value,omitempty"`
	ScopeLabel           string               `json:"scope_label,omitempty"`
	ScopeValue           string               `json:"scope_value,omitempty"`
	CacheKey             string               `json:"cache_key,omitempty"`
	CacheLabel           string               `json:"cache_label,omitempty"`
	SessionDecisionNote  string               `json:"session_decision_note,omitempty"`
	ProjectDecisionNote  string               `json:"project_decision_note,omitempty"`
	ProposedPermissions  *PermissionProfile   `json:"proposed_permissions,omitempty"`
	ProposedAmendment    *ExecPolicyAmendment `json:"proposed_amendment,omitempty"`
	RequestedAt          time.Time            `json:"requested_at"`
}

// ApprovalDecision 描述一次审批结果。
type ApprovalDecision struct {
	RequestID          string               `json:"request_id"`
	Type               ApprovalDecisionType `json:"type,omitempty"`
	Approved           bool                 `json:"approved"`
	Reason             string               `json:"reason,omitempty"`
	Source             string               `json:"source,omitempty"`
	GrantedPermissions *PermissionProfile   `json:"granted_permissions,omitempty"`
	PolicyAmendment    *ExecPolicyAmendment `json:"policy_amendment,omitempty"`
	DecidedAt          time.Time            `json:"decided_at"`
}

// ApprovalEvent 是审批生命周期事件。
type ApprovalEvent struct {
	SessionID string            `json:"session_id,omitempty"`
	Type      string            `json:"type"` // "requested" | "resolved"
	Request   ApprovalRequest   `json:"request"`
	Decision  *ApprovalDecision `json:"decision,omitempty"`
}

// FormatApprovalPrompt 将审批请求格式化为适合直接展示给用户的文案。
func FormatApprovalPrompt(req *ApprovalRequest) string {
	if req == nil {
		return "Allow requested action?"
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Allow requested action?"
	}
	details := make([]string, 0, 5)
	if req.ToolName != "" {
		details = append(details, "tool="+req.ToolName)
	}
	if req.Risk != "" {
		details = append(details, "risk="+req.Risk)
	}
	if req.Reason != "" {
		details = append(details, "reason="+req.Reason)
	}
	if req.ReasonCode != "" {
		details = append(details, "reason_code="+req.ReasonCode)
	}
	if req.Enforcement != "" {
		details = append(details, "enforcement="+string(req.Enforcement))
	}
	if len(details) == 0 {
		return prompt
	}
	return fmt.Sprintf("%s (%s)", prompt, strings.Join(details, "; "))
}

func NormalizeApprovalDecision(decision *ApprovalDecision) *ApprovalDecision {
	if decision == nil {
		return nil
	}
	if decision.Type == "" {
		if decision.Approved {
			decision.Type = ApprovalDecisionApprove
		} else {
			decision.Type = ApprovalDecisionDeny
		}
	}
	switch decision.Type {
	case ApprovalDecisionApprove, ApprovalDecisionApproveSession, ApprovalDecisionPolicyAmendment, ApprovalDecisionGrantPermission:
		decision.Approved = true
	default:
		decision.Approved = false
		if decision.Type == "" {
			decision.Type = ApprovalDecisionDeny
		}
	}
	return decision
}

// FormatDeniedMessage 生成适合直接展示给用户的拒绝文案。
func FormatDeniedMessage(toolName, reason, reasonCode string, enforcement EnforcementMode) string {
	base := "Tool call denied by policy."
	if strings.TrimSpace(toolName) != "" {
		base = fmt.Sprintf("Tool %s denied by policy.", toolName)
	}
	details := make([]string, 0, 3)
	if strings.TrimSpace(reason) != "" {
		details = append(details, "reason="+strings.TrimSpace(reason))
	}
	if strings.TrimSpace(reasonCode) != "" {
		details = append(details, "reason_code="+strings.TrimSpace(reasonCode))
	}
	if enforcement != "" {
		details = append(details, "enforcement="+string(enforcement))
	}
	if len(details) == 0 {
		return base
	}
	return base + " (" + strings.Join(details, "; ") + ")"
}
