package interaction

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

type DecisionScope string

const (
	DecisionScopeOnce    DecisionScope = "once"
	DecisionScopeSession DecisionScope = "session"
	DecisionScopeProject DecisionScope = "project"
)

type DecisionPersistence string

const (
	DecisionPersistenceRequest DecisionPersistence = "request"
	DecisionPersistenceSession DecisionPersistence = "session"
	DecisionPersistenceProject DecisionPersistence = "project"
)

type RuleBinding struct {
	Category    ApprovalCategory       `json:"category,omitempty"`
	ToolName    string                 `json:"tool_name,omitempty"`
	Label       string                 `json:"label,omitempty"`
	Match       string                 `json:"match,omitempty"`
	CacheKey    string                 `json:"cache_key,omitempty"`
	Persistence DecisionPersistence    `json:"persistence,omitempty"`
	CommandRule *ExecPolicyCommandRule `json:"command_rule,omitempty"`
	HTTPRule    *ExecPolicyHTTPRule    `json:"http_rule,omitempty"`
}

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
	ID                  string               `json:"id"`
	Kind                ApprovalKind         `json:"kind"`
	Category            ApprovalCategory     `json:"category,omitempty"`
	SessionID           string               `json:"session_id,omitempty"`
	ToolName            string               `json:"tool_name,omitempty"`
	Risk                string               `json:"risk,omitempty"`
	Prompt              string               `json:"prompt"`
	Reason              string               `json:"reason,omitempty"`
	ReasonCode          string               `json:"reason_code,omitempty"`
	Enforcement         EnforcementMode      `json:"enforcement,omitempty"`
	Input               json.RawMessage      `json:"input,omitempty"`
	ActionLabel         string               `json:"action_label,omitempty"`
	ActionValue         string               `json:"action_value,omitempty"`
	ScopeLabel          string               `json:"scope_label,omitempty"`
	ScopeValue          string               `json:"scope_value,omitempty"`
	AllowedScopes       []DecisionScope      `json:"allowed_scopes,omitempty"`
	DefaultScope        DecisionScope        `json:"default_scope,omitempty"`
	DefaultPersistence  DecisionPersistence  `json:"default_persistence,omitempty"`
	CacheKey            string               `json:"cache_key,omitempty"`
	CacheLabel          string               `json:"cache_label,omitempty"`
	RuleBinding         *RuleBinding         `json:"rule_binding,omitempty"`
	SessionDecisionNote string               `json:"session_decision_note,omitempty"`
	ProjectDecisionNote string               `json:"project_decision_note,omitempty"`
	ProposedPermissions *PermissionProfile   `json:"proposed_permissions,omitempty"`
	ProposedAmendment   *ExecPolicyAmendment `json:"proposed_amendment,omitempty"`
	RequestedAt         time.Time            `json:"requested_at"`
}

// ApprovalDecision 描述一次审批结果。
type ApprovalDecision struct {
	RequestID          string               `json:"request_id"`
	Type               ApprovalDecisionType `json:"type,omitempty"`
	Approved           bool                 `json:"approved"`
	Reason             string               `json:"reason,omitempty"`
	Source             string               `json:"source,omitempty"`
	Scope              DecisionScope        `json:"scope,omitempty"`
	Persistence        DecisionPersistence  `json:"persistence,omitempty"`
	CacheKey           string               `json:"cache_key,omitempty"`
	RuleBinding        *RuleBinding         `json:"rule_binding,omitempty"`
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
	return NormalizeApprovalDecisionForRequest(nil, decision)
}

func NormalizeApprovalRequest(req *ApprovalRequest) *ApprovalRequest {
	if req == nil {
		return nil
	}
	req.ToolName = strings.TrimSpace(req.ToolName)
	req.ScopeLabel = strings.TrimSpace(req.ScopeLabel)
	req.ScopeValue = strings.TrimSpace(req.ScopeValue)
	req.CacheKey = strings.TrimSpace(req.CacheKey)
	req.CacheLabel = strings.TrimSpace(req.CacheLabel)
	req.AllowedScopes = normalizeDecisionScopes(req.AllowedScopes)
	if len(req.AllowedScopes) == 0 {
		req.AllowedScopes = []DecisionScope{DecisionScopeOnce}
		if strings.TrimSpace(req.SessionDecisionNote) != "" {
			req.AllowedScopes = append(req.AllowedScopes, DecisionScopeSession)
		}
		if strings.TrimSpace(req.ProjectDecisionNote) != "" {
			req.AllowedScopes = append(req.AllowedScopes, DecisionScopeProject)
		}
	}
	if req.DefaultScope == "" {
		req.DefaultScope = req.AllowedScopes[0]
	}
	if req.DefaultPersistence == "" {
		req.DefaultPersistence = defaultPersistenceForScope(req.DefaultScope)
	}
	if req.RuleBinding == nil && (req.CacheKey != "" || req.ScopeValue != "") {
		req.RuleBinding = &RuleBinding{
			Category:    req.Category,
			ToolName:    req.ToolName,
			Label:       firstNonEmptyString(req.ScopeLabel, req.CacheLabel),
			Match:       req.ScopeValue,
			CacheKey:    req.CacheKey,
			Persistence: req.DefaultPersistence,
		}
	}
	return req
}

func NormalizeApprovalDecisionForRequest(req *ApprovalRequest, decision *ApprovalDecision) *ApprovalDecision {
	if decision == nil {
		return nil
	}
	req = NormalizeApprovalRequest(req)
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
	if decision.Scope == "" {
		decision.Scope = defaultScopeForDecision(req, decision.Type)
	}
	if decision.Persistence == "" {
		decision.Persistence = defaultPersistenceForScope(decision.Scope)
	}
	if decision.CacheKey == "" && req != nil {
		decision.CacheKey = req.CacheKey
	}
	if decision.RuleBinding == nil && req != nil && req.RuleBinding != nil {
		binding := *req.RuleBinding
		if binding.Persistence == "" {
			binding.Persistence = decision.Persistence
		}
		decision.RuleBinding = &binding
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

func normalizeDecisionScopes(scopes []DecisionScope) []DecisionScope {
	if len(scopes) == 0 {
		return nil
	}
	seen := make(map[DecisionScope]struct{}, len(scopes))
	out := make([]DecisionScope, 0, len(scopes))
	for _, scope := range scopes {
		switch scope {
		case DecisionScopeOnce, DecisionScopeSession, DecisionScopeProject:
		default:
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func defaultScopeForDecision(req *ApprovalRequest, typ ApprovalDecisionType) DecisionScope {
	switch typ {
	case ApprovalDecisionApproveSession, ApprovalDecisionGrantPermission:
		return DecisionScopeSession
	case ApprovalDecisionPolicyAmendment:
		return DecisionScopeProject
	case ApprovalDecisionApprove:
		if req != nil && req.DefaultScope != "" {
			return req.DefaultScope
		}
		return DecisionScopeOnce
	default:
		return DecisionScopeOnce
	}
}

func defaultPersistenceForScope(scope DecisionScope) DecisionPersistence {
	switch scope {
	case DecisionScopeSession:
		return DecisionPersistenceSession
	case DecisionScopeProject:
		return DecisionPersistenceProject
	default:
		return DecisionPersistenceRequest
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
