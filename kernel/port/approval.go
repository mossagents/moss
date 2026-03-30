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

// ApprovalRequest 描述一次结构化审批请求。
type ApprovalRequest struct {
	ID          string          `json:"id"`
	Kind        ApprovalKind    `json:"kind"`
	SessionID   string          `json:"session_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Risk        string          `json:"risk,omitempty"`
	Prompt      string          `json:"prompt"`
	Reason      string          `json:"reason,omitempty"`
	ReasonCode  string          `json:"reason_code,omitempty"`
	Enforcement EnforcementMode `json:"enforcement,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	RequestedAt time.Time       `json:"requested_at"`
}

// ApprovalDecision 描述一次审批结果。
type ApprovalDecision struct {
	RequestID string    `json:"request_id"`
	Approved  bool      `json:"approved"`
	Reason    string    `json:"reason,omitempty"`
	Source    string    `json:"source,omitempty"`
	DecidedAt time.Time `json:"decided_at"`
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
