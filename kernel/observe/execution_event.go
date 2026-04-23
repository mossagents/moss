package observe

import (
	"time"

	"github.com/mossagents/moss/kernel/io"
)

// ExecutionEventType 表示统一执行事件类型。
type ExecutionEventType string

const (
	ExecutionRunStarted          ExecutionEventType = "run.started"
	ExecutionRunCompleted        ExecutionEventType = "run.completed"
	ExecutionRunFailed           ExecutionEventType = "run.failed"
	ExecutionRunCancelled        ExecutionEventType = "run.cancelled"
	ExecutionIterationStarted    ExecutionEventType = "iteration.started"
	ExecutionIterationProgress   ExecutionEventType = "iteration.progress"
	ExecutionLLMStarted          ExecutionEventType = "llm.started"
	ExecutionLLMCompleted        ExecutionEventType = "llm.completed"
	ExecutionToolStarted         ExecutionEventType = "tool.started"
	ExecutionToolCompleted       ExecutionEventType = "tool.completed"
	ExecutionHostedToolStarted   ExecutionEventType = "hosted_tool.started"
	ExecutionHostedToolProgress  ExecutionEventType = "hosted_tool.progress"
	ExecutionHostedToolCompleted ExecutionEventType = "hosted_tool.completed"
	ExecutionHostedToolFailed    ExecutionEventType = "hosted_tool.failed"
	ExecutionPolicyRuleMatched   ExecutionEventType = "policy.rule_matched"
	ExecutionApprovalRequest     ExecutionEventType = "approval.requested"
	ExecutionApprovalResolved    ExecutionEventType = "approval.resolved"
	ExecutionContextCompacted    ExecutionEventType = "context.compacted"
	ExecutionContextTrimRetry    ExecutionEventType = "context.trim_retry"
	ExecutionContextNormalized   ExecutionEventType = "context.normalized"
	ExecutionGuardianReviewed    ExecutionEventType = "guardian.reviewed"
	ExecutionSnapshotCreated     ExecutionEventType = "snapshot.created"
	ExecutionCheckpointCreated   ExecutionEventType = "checkpoint.created"
	ExecutionSessionForked       ExecutionEventType = "session.forked"
	ExecutionReplayPrepared      ExecutionEventType = "replay.prepared"
	ExecutionSwarmStarted        ExecutionEventType = "swarm.started"
	ExecutionSwarmThreadSpawned  ExecutionEventType = "swarm.thread_spawned"
	ExecutionSwarmTaskCreated    ExecutionEventType = "swarm.task_created"
	ExecutionSwarmTaskClaimed    ExecutionEventType = "swarm.task_claimed"
	ExecutionSwarmMessageSent    ExecutionEventType = "swarm.message_sent"
	ExecutionSwarmArtifactPub    ExecutionEventType = "swarm.artifact_published"
	ExecutionSwarmThreadDone     ExecutionEventType = "swarm.thread_completed"
	ExecutionSwarmCompleted      ExecutionEventType = "swarm.completed"
	ExecutionSwarmFailed         ExecutionEventType = "swarm.failed"
)

// ExecutionEvent 是运行时统一结构化事件。
type ExecutionEvent struct {
	Type         ExecutionEventType `json:"type"`
	EventID      string             `json:"event_id,omitempty"`
	EventVersion int                `json:"event_version,omitempty"`
	RunID        string             `json:"run_id,omitempty"`
	TurnID       string             `json:"turn_id,omitempty"`
	ParentID     string             `json:"parent_id,omitempty"`
	SessionID    string             `json:"session_id,omitempty"`
	Timestamp    time.Time          `json:"timestamp"`
	Phase        string             `json:"phase,omitempty"`
	Actor        string             `json:"actor,omitempty"`
	PayloadKind  string             `json:"payload_kind,omitempty"`
	ToolName     string             `json:"tool_name,omitempty"`
	CallID       string             `json:"call_id,omitempty"`
	Risk         string             `json:"risk,omitempty"`
	// MCPServerID / MCPToolName MCP 工具扩展字段（§14.3）。
	MCPServerID string             `json:"mcp_server_id,omitempty"`
	MCPToolName string             `json:"mcp_tool_name,omitempty"`
	ReasonCode  string             `json:"reason_code,omitempty"`
	Enforcement io.EnforcementMode `json:"enforcement,omitempty"`
	Model       string             `json:"model,omitempty"`
	Duration    time.Duration      `json:"duration,omitempty"`
	Error       string             `json:"error,omitempty"`
	Metadata    map[string]any     `json:"data,omitempty"`
}
