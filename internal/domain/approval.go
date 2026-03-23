package domain

import "time"

type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
)

type ApprovalRequest struct {
	ApprovalID  string
	RunID       string
	TaskID      string
	ToolName    string
	Description string
	Input       map[string]any
	Status      ApprovalStatus
	RequestedAt time.Time
	ResolvedAt  *time.Time
	Reason      string
}
