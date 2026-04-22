// Package runtime 实现以内核为中心的单路径 agent runtime。
// 本包对应 spec: docs/superpowers/specs/2026-04-21-kernel-centric-agent-runtime-unification-design.md
package runtime

import (
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// ─────────────────────────────────────────────
// 事件类型常量（§5.3）
// ─────────────────────────────────────────────

// EventType 标识 RuntimeEvent 的具体种类。
type EventType string

const (
	EventTypeSessionCreated     EventType = "session_created"
	EventTypeTurnStarted        EventType = "turn_started"
	EventTypeTurnCompleted      EventType = "turn_completed"
	EventTypePromptMaterialized EventType = "prompt_materialized"
	EventTypeToolCalled         EventType = "tool_called"
	EventTypeToolCompleted      EventType = "tool_completed"
	EventTypeApprovalRequested  EventType = "approval_requested"
	EventTypeApprovalResolved   EventType = "approval_resolved"
	EventTypePermissionsAmended EventType = "permissions_amended"
	EventTypeContextCompacted   EventType = "context_compacted"
	EventTypeSessionForked      EventType = "session_forked"
	EventTypeCheckpointCreated  EventType = "checkpoint_created"
	EventTypeSessionCompleted   EventType = "session_completed"
	EventTypeSessionFailed      EventType = "session_failed"
	EventTypeTaskStarted        EventType = "task_started"
	EventTypeTaskCompleted      EventType = "task_completed"
	EventTypeTaskAbandoned      EventType = "task_abandoned"
	EventTypeRoleTransitioned   EventType = "role_transitioned"
	EventTypePlanUpdated        EventType = "plan_updated"
	EventTypeMemoryConsolidated EventType = "memory_consolidated"
	EventTypeBudgetExhausted    EventType = "budget_exhausted"
	EventTypeBudgetLimitUpdated EventType = "budget_limit_updated"
	EventTypeSubagentSpawned    EventType = "subagent_spawned"
	EventTypeSubagentCompleted  EventType = "subagent_completed"
	// EventTypeLLMCalled 对应一次 LLM 调用完成事件（§14.8 Provider Failover）。
	EventTypeLLMCalled EventType = "llm_called"
)

// ─────────────────────────────────────────────
// RuntimeEvent — 唯一事实来源（§5.3）
// ─────────────────────────────────────────────

// RuntimeEvent 是系统内唯一的事实记录单元。
// 每个事件必须带 SessionID、Seq、Timestamp、Type，
// Payload 承载该事件类型的具体字段。
type RuntimeEvent struct {
	// 公共字段（每个事件必须携带）
	SessionID string    `json:"session_id"`
	Seq       int64     `json:"seq"`
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`

	// BlueprintVersion 用于可恢复事件的 blueprint 版本校验。
	// event store 不得接受无 BlueprintVersion 的可恢复事件。
	BlueprintVersion string `json:"blueprint_version,omitempty"`

	// RequestID 用于幂等保障；同一批次追加的事件共享同一 RequestID。
	RequestID string `json:"request_id,omitempty"`

	// Payload 包含事件类型特定字段，使用具体结构体（见下方各 Payload 类型）。
	Payload any `json:"payload,omitempty"`
}

// ─────────────────────────────────────────────
// Payload 结构体定义
// ─────────────────────────────────────────────

// SessionCreatedPayload 对应 session_created 事件。
type SessionCreatedPayload struct {
	// BlueprintPayload 持久化 canonical blueprint 完整内容（§5.3 恢复相关硬约束）。
	BlueprintPayload *SessionBlueprint `json:"blueprint_payload"`
	// TriggerSource 区分 session 来源（§14.5 调度子系统）。
	// 取值：interactive / scheduled / api / resume
	TriggerSource string `json:"trigger_source,omitempty"`
	// ParentSessionID 若非空，表示该 session 由父 subagent 派生（§5.3 subagent 约束）。
	ParentSessionID string `json:"parent_session_id,omitempty"`
}

// TurnStartedPayload 对应 turn_started 事件。
type TurnStartedPayload struct {
	TurnID string `json:"turn_id"`
}

// TurnOutcome 描述 turn 的结束原因（§5.3 turn_completed 约束）。
type TurnOutcome string

const (
	TurnOutcomeCompleted            TurnOutcome = "completed"
	TurnOutcomeSuspendedForApproval TurnOutcome = "suspended_for_approval"
	TurnOutcomeBudgetExhausted      TurnOutcome = "budget_exhausted"
	TurnOutcomeError                TurnOutcome = "error"
)

// TurnCompletedPayload 对应 turn_completed 事件（§5.3 turn_completed 约束）。
// 每个 turn_started 必须对应一个 turn_completed，标识 turn 事件边界。
type TurnCompletedPayload struct {
	TurnID string `json:"turn_id"`
	// Outcome 标识结束原因。
	Outcome TurnOutcome `json:"outcome"`
	// ModelResponseRef 指向模型原始响应内容的可寻址引用（文本 + 工具调用声明）。
	// review 和 replay 通过此字段重建"模型当时说了什么"。
	ModelResponseRef string `json:"model_response_ref,omitempty"`
	// ErrorKind 仅 Outcome = error 时有意义。
	ErrorKind string `json:"error_kind,omitempty"`
}

// BudgetSnapshot 记录预算使用快照（§14.4、§14.10）。
type BudgetSnapshot struct {
	MainTokensUsed          int `json:"main_tokens_used"`
	MainTokensRemaining     int `json:"main_tokens_remaining"`
	ThinkingTokensUsed      int `json:"thinking_tokens_used,omitempty"`
	ThinkingTokensRemaining int `json:"thinking_tokens_remaining,omitempty"`
	StepsUsed               int `json:"steps_used,omitempty"`
	StepsRemaining          int `json:"steps_remaining,omitempty"`
}

// PromptMaterializedPayload 对应 prompt_materialized 事件（§5.5 物化契约）。
type PromptMaterializedPayload struct {
	PromptMaterializedID string         `json:"prompt_materialized_id"`
	PromptHash           string         `json:"prompt_hash"`
	PolicyHash           string         `json:"policy_hash"`
	SelectedLayerIDs     []string       `json:"selected_layer_ids"`
	TruncatedLayerIDs    []string       `json:"truncated_layer_ids,omitempty"`
	BudgetSnapshot       BudgetSnapshot `json:"budget_snapshot"`
	// ProviderProvenance 记录 compiler 来源（resolver_build_version 等）。
	ProviderProvenance map[string]string `json:"provider_provenance,omitempty"`
	// ProviderID 实际使用的 LLM provider（§5.3 模型调用相关事件）。
	ProviderID string `json:"provider_id,omitempty"`
	// ModelID 实际调用的模型名称（含 failover 后实际模型）。
	ModelID string `json:"model_id,omitempty"`
	// OriginalProviderID failover 时首选 provider（§14.8）。仅发生 failover 时设置。
	OriginalProviderID string `json:"original_provider_id,omitempty"`
}

// ToolCalledPayload 对应 tool_called 事件。
type ToolCalledPayload struct {
	ToolCallID           string         `json:"tool_call_id"`
	ToolName             string         `json:"tool_name"`
	Arguments            map[string]any `json:"arguments,omitempty"`
	PolicyHash           string         `json:"policy_hash,omitempty"`
	PromptMaterializedID string         `json:"prompt_materialized_id"`
	// MCP 工具扩展字段（§14.3）
	MCPServerID string `json:"mcp_server_id,omitempty"`
	MCPToolName string `json:"mcp_tool_name,omitempty"`
}

// ToolCompletedPayload 对应 tool_completed 事件。
type ToolCompletedPayload struct {
	ToolCallID   string              `json:"tool_call_id"`
	ToolName     string              `json:"tool_name"`
	ResultParts  []model.ContentPart `json:"result_parts,omitempty"`
	IsError      bool                `json:"is_error,omitempty"`
	ErrorMessage string              `json:"error_message,omitempty"`
	// MCP 工具扩展字段（§14.3），与 tool_called 对称。
	MCPServerID string `json:"mcp_server_id,omitempty"`
	MCPToolName string `json:"mcp_tool_name,omitempty"`
}

// ApprovalRequestedPayload 对应 approval_requested 事件。
type ApprovalRequestedPayload struct {
	ApprovalID string `json:"approval_id"`
	PolicyHash string `json:"policy_hash"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// LLMCalledPayload 对应 llm_called 事件（§14.8 Provider Failover）。
// 记录一次 LLM 调用完成的关键信息，包含 failover 审计字段。
type LLMCalledPayload struct {
	// TurnID 关联的 turn 标识。
	TurnID string `json:"turn_id,omitempty"`
	// PromptMaterializedID 触发此 LLM 调用的 prompt 物化 ID。
	PromptMaterializedID string `json:"prompt_materialized_id,omitempty"`
	// ModelID 实际命中的模型（含 failover 后）。
	ModelID string `json:"model_id,omitempty"`
	// ProviderID 实际使用的 LLM provider（§14.8）。
	ProviderID string `json:"provider_id,omitempty"`
	// OriginalProviderID failover 前首选 provider（§14.8）。仅发生 failover 时设置。
	OriginalProviderID string `json:"original_provider_id,omitempty"`
	// TokensUsed 本次调用消耗的总 token 数。
	TokensUsed int `json:"tokens_used,omitempty"`
	// ThinkingTokensUsed extended thinking 消耗的 token 数（§14.10）。
	ThinkingTokensUsed int `json:"thinking_tokens_used,omitempty"`
	// StopReason LLM 停止原因（end_turn / tool_use / max_tokens）。
	StopReason string `json:"stop_reason,omitempty"`
	// IsError 若 true 表示此次 LLM 调用以错误结束。
	IsError bool `json:"is_error,omitempty"`
	// ErrorMessage 错误描述，仅 IsError=true 时设置。
	ErrorMessage string `json:"error_message,omitempty"`
}

// ResolverType 标识审批决策来源（§14.2 Guardian）。
type ResolverType string

const (
	ResolverTypeHuman    ResolverType = "human"
	ResolverTypeGuardian ResolverType = "guardian"
	ResolverTypePolicy   ResolverType = "policy"
)

// ApprovalResolvedPayload 对应 approval_resolved 事件。
type ApprovalResolvedPayload struct {
	ApprovalID   string       `json:"approval_id"`
	PolicyHash   string       `json:"policy_hash"`
	ResolverType ResolverType `json:"resolver_type"`
	Approved     bool         `json:"approved"`
	Reason       string       `json:"reason,omitempty"`
	// CacheKey 对应 ApprovalRequest.CacheKey，用于跨重启的审批 cache 查询。
	// 非空时表示此决议可在同 session 后续请求中复用（approve_for_session/grant_permission 等）。
	CacheKey string `json:"cache_key,omitempty"`
	// ToolName 被审批工具的名称。
	ToolName string `json:"tool_name,omitempty"`
	// DecisionType 决议类型（approve/approve_for_session/grant_permission/policy_amendment/deny）。
	DecisionType string `json:"decision_type,omitempty"`
}

// PermissionsAmendedPayload 对应 permissions_amended 事件。
type PermissionsAmendedPayload struct {
	PolicyHash string `json:"policy_hash"`
	Amendment  string `json:"amendment,omitempty"`
}

// ContextCompactedPayload 对应 context_compacted 事件。
type ContextCompactedPayload struct {
	CompactedItemCount int    `json:"compacted_item_count"`
	SummaryItemID      string `json:"summary_item_id"`
}

// SessionForkedPayload 对应 session_forked 事件（§5.3 恢复相关硬约束）。
type SessionForkedPayload struct {
	ChildSessionID   string            `json:"child_session_id"`
	BlueprintPayload *SessionBlueprint `json:"blueprint_payload"`
}

// CheckpointCreatedPayload 对应 checkpoint_created 事件（§5.3 checkpoint 边界要求）。
type CheckpointCreatedPayload struct {
	CheckpointID     string            `json:"checkpoint_id"`
	EventBoundarySeq int64             `json:"event_boundary_seq"`
	BlueprintPayload *SessionBlueprint `json:"blueprint_payload,omitempty"`
	BlueprintRef     string            `json:"blueprint_ref,omitempty"`
	// WorkspaceSnapshotRef 可选，指向 git commit hash 或 worktree snapshot id（§14.7）。
	WorkspaceSnapshotRef string `json:"workspace_snapshot_ref,omitempty"`
}

// SessionCompletedPayload 对应 session_completed 事件。
type SessionCompletedPayload struct {
	Summary string `json:"summary,omitempty"`
}

// SessionFailedPayload 对应 session_failed 事件（§5.3 session_failed 约束）。
type SessionFailedPayload struct {
	ErrorKind    string `json:"error_kind"`
	ErrorMessage string `json:"error_message"`
	// LastSeq 是中断时最后成功写入的事件 seq。
	LastSeq int64 `json:"last_seq"`
}

// TaskStartedPayload 对应 task_started 事件。
type TaskStartedPayload struct {
	TaskID string `json:"task_id"`
	// ClaimedBy 标识认领该 task 的 worker（§14.6 分布式 TaskRuntime）。
	ClaimedBy string `json:"claimed_by,omitempty"`
	// PlanningItemID 建立 task 与 planning.Item 的双向关联（§5.3 task 事件约束）。
	PlanningItemID string `json:"planning_item_id,omitempty"`
}

// PromoteScopedItem 描述 task 结束时对 task-scoped item 的 promote 决策（§5.3）。
type PromoteScopedItem struct {
	ItemID  string `json:"item_id"`
	Promote bool   `json:"promote"`
	// Summary 仅 Promote=true 时必填。
	Summary string `json:"summary,omitempty"`
}

// TaskCompletedPayload 对应 task_completed 事件。
type TaskCompletedPayload struct {
	TaskID string `json:"task_id"`
	// ResultRef 执行结果的可寻址引用（§14.6）。
	ResultRef string `json:"result_ref,omitempty"`
	// PromoteScopedItems 声明该 task 下 task-scoped item 的 promote 决策（§5.3）。
	PromoteScopedItems []PromoteScopedItem `json:"promote_scoped_items,omitempty"`
}

// TaskAbandonedPayload 对应 task_abandoned 事件。
type TaskAbandonedPayload struct {
	TaskID             string              `json:"task_id"`
	Reason             string              `json:"reason,omitempty"`
	ResultRef          string              `json:"result_ref,omitempty"`
	PromoteScopedItems []PromoteScopedItem `json:"promote_scoped_items,omitempty"`
}

// RoleTransitionedPayload 对应 role_transitioned 事件（§5.3 role_transitioned 约束）。
type RoleTransitionedPayload struct {
	FromRoleOverlayID string `json:"from_role_overlay_id"`
	ToRoleOverlayID   string `json:"to_role_overlay_id"`
	TransitionReason  string `json:"transition_reason,omitempty"`
}

// PlanUpdatedPayload 对应 plan_updated 事件（§5.3 plan_updated 约束）。
type PlanUpdatedPayload struct {
	// PlanningStateSnapshot 持久化完整 planning state 快照。
	PlanningStateSnapshot any    `json:"planning_state_snapshot"`
	TaskBoundaryEventID   string `json:"task_boundary_event_id,omitempty"`
}

// MemoryConsolidatedPayload 对应 memory_consolidated 事件（§5.3 memory_consolidated 约束）。
type MemoryConsolidatedPayload struct {
	MemoryRecordID string `json:"memory_record_id"`
	MemoryPath     string `json:"memory_path"`
}

// BudgetKind 标识预算类型（§14.4、§14.10）。
type BudgetKind string

const (
	BudgetKindToken         BudgetKind = "token"
	BudgetKindThinkingToken BudgetKind = "thinking_token"
	BudgetKindStep          BudgetKind = "step"
	BudgetKindTime          BudgetKind = "time"
)

// BudgetExhaustedPayload 对应 budget_exhausted 事件（§5.3 budget_exhausted 约束）。
type BudgetExhaustedPayload struct {
	BudgetKind    BudgetKind `json:"budget_kind"`
	ConsumedValue int64      `json:"consumed_value"`
	LimitValue    int64      `json:"limit_value"`
}

// BudgetLimitUpdatedPayload 对应 budget_limit_updated 事件。
type BudgetLimitUpdatedPayload struct {
	PreviousMainTokenBudget int64  `json:"previous_main_token_budget"`
	MainTokenBudget         int64  `json:"main_token_budget"`
	Reason                  string `json:"reason,omitempty"`
}

// SubagentSpawnedPayload 对应 subagent_spawned 事件（§5.3 subagent 约束）。
type SubagentSpawnedPayload struct {
	ChildSessionID    string `json:"child_session_id"`
	ParentTaskEventID string `json:"parent_task_event_id,omitempty"`
}

// SubagentOutcome 描述 subagent 的结束结果。
type SubagentOutcome string

const (
	SubagentOutcomeSuccess   SubagentOutcome = "success"
	SubagentOutcomeFailed    SubagentOutcome = "failed"
	SubagentOutcomeAbandoned SubagentOutcome = "abandoned"
)

// SubagentCompletedPayload 对应 subagent_completed 事件（§5.3 subagent 约束）。
type SubagentCompletedPayload struct {
	ChildSessionID string          `json:"child_session_id"`
	ResultRef      string          `json:"result_ref,omitempty"`
	Outcome        SubagentOutcome `json:"outcome"`
}
