package port

import (
	"encoding/json"
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
