package runtime

import (
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// MaterializedState（§5.4）
// ─────────────────────────────────────────────

// HistoryItemScope 标识 history item 的生命周期。
type HistoryItemScope string

const (
	HistoryItemScopePersistent HistoryItemScope = "persistent"
	HistoryItemScopeTaskScoped HistoryItemScope = "task-scoped"
)

// HistoryItem 是可重放 history 中的一条记录（§4.4、§7.3）。
type HistoryItem struct {
	ItemID string           `json:"item_id"`
	Scope  HistoryItemScope `json:"scope"`
	// BoundToTaskEventID 仅 Scope=task-scoped 时有意义（§4.4 task-scoped 规则）。
	// 必须写入 EventStore，不允许只在内存中约定。
	BoundToTaskEventID string `json:"bound_to_task_event_id,omitempty"`
	// Active 标识该 item 是否仍在有效期内。
	// task-scoped item 在 task retire 后置 false。
	Active  bool           `json:"active"`
	Content HistoryContent `json:"content"`
	// OriginSeq 该 item 来自哪个 RuntimeEvent 的 seq。
	OriginSeq int64 `json:"origin_seq"`
}

// HistoryContent 持有 history item 的内容（文本摘要或结构化内容）。
type HistoryContent struct {
	Text string `json:"text,omitempty"`
	Role string `json:"role,omitempty"`
}

// ActiveTask 记录当前活跃的 task（用于 task-scoped 逻辑）。
type ActiveTask struct {
	TaskID             string    `json:"task_id"`
	TaskStartedEventID string    `json:"task_started_event_id"`
	TaskStartedSeq     int64     `json:"task_started_seq"`
	PlanningItemID     string    `json:"planning_item_id,omitempty"`
	ClaimedBy          string    `json:"claimed_by,omitempty"`
	StartedAt          time.Time `json:"started_at"`
}

// PlanningState 记录当前 planning 状态（从 plan_updated 事件 projection）。
type PlanningState struct {
	// Snapshot 持有最新 plan_updated 事件中的完整 planning state 快照。
	Snapshot any   `json:"snapshot,omitempty"`
	LastSeq  int64 `json:"last_seq"`
}

// ApprovalStatus 标识当前 session 的审批状态。
type ApprovalStatus string

const (
	ApprovalStatusNone     ApprovalStatus = "none"
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusResolved ApprovalStatus = "resolved"
)

// MaterializedState 是从事件流投影出的运行时状态（§5.4）。
// 它提供 session 当前视图，但不得越权成为事实来源。
type MaterializedState struct {
	SessionID string `json:"session_id"`

	// Blueprint 当前 session 的 canonical blueprint。
	Blueprint *SessionBlueprint `json:"blueprint,omitempty"`

	// CurrentSeq 已处理的最新事件 seq。
	CurrentSeq int64 `json:"current_seq"`

	// Status session 的当前状态。
	Status string `json:"status"`

	// PersistentHistory persistent scope 的 history items（有序）。
	PersistentHistory []HistoryItem `json:"persistent_history,omitempty"`

	// TaskScopedHistory task-scoped scope 的 history items，key 为 bound_to_task_event_id。
	TaskScopedHistory map[string][]HistoryItem `json:"task_scoped_history,omitempty"`

	// ActiveTasks 当前活跃的 tasks，key 为 task_id。
	ActiveTasks map[string]*ActiveTask `json:"active_tasks,omitempty"`

	// PlanningState 当前 planning 状态。
	PlanningState *PlanningState `json:"planning_state,omitempty"`

	// CurrentTurnID 当前活跃 turn 的 ID（turn_started 后、turn_completed 前）。
	CurrentTurnID string `json:"current_turn_id,omitempty"`

	// CurrentApproval 当前待处理的审批（若有）。
	CurrentApproval *PendingApproval `json:"current_approval,omitempty"`

	// ResolvedApprovals 本 session 内已决议且有 cache_key 的审批记录。
	// 用于跨重启的审批 cache 查询，替代 session.ApprovalState 的旧路径。
	ResolvedApprovals []ResolvedApprovalEntry `json:"resolved_approvals,omitempty"`

	// EffectiveToolPolicy 当前有效的工具权限策略。
	EffectiveToolPolicy *EffectiveToolPolicy `json:"effective_tool_policy,omitempty"`

	// LastPromptMaterialized 最近一次 prompt_materialized 事件的摘要。
	LastPromptMaterialized *PromptMaterializedSummary `json:"last_prompt_materialized,omitempty"`
}

// PendingApproval 记录当前待处理审批。
type PendingApproval struct {
	ApprovalID string `json:"approval_id"`
	PolicyHash string `json:"policy_hash"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ResolvedApprovalEntry 记录一条已决议的审批（用于跨重启的 cache 查询）。
type ResolvedApprovalEntry struct {
	// CacheKey 对应 ApprovalRequest.CacheKey，非空才会被记录。
	CacheKey     string    `json:"cache_key"`
	ToolName     string    `json:"tool_name,omitempty"`
	DecisionType string    `json:"decision_type,omitempty"`
	Approved     bool      `json:"approved"`
	ResolvedAt   time.Time `json:"resolved_at"`
}

// PromptMaterializedSummary 最近一次 prompt 物化的摘要信息。
type PromptMaterializedSummary struct {
	PromptMaterializedID string `json:"prompt_materialized_id"`
	PromptHash           string `json:"prompt_hash"`
	Seq                  int64  `json:"seq"`
}

// ─────────────────────────────────────────────
// ProjectionEngine（§7.3、§4.4）
// ─────────────────────────────────────────────

// ProjectionEngine 是唯一允许从 RuntimeEvent 流更新 MaterializedState 的组件。
// 它统一执行 task-scoped retire 逻辑，不允许各 projector 分散实现。
type ProjectionEngine struct{}

// NewProjectionEngine 创建 ProjectionEngine 实例。
func NewProjectionEngine() *ProjectionEngine {
	return &ProjectionEngine{}
}

// Apply 将单个事件应用到 MaterializedState，更新投影。
// 调用方在 AppendEvents 成功后调用此方法（或在 RebuildProjections 时批量调用）。
func (e *ProjectionEngine) Apply(state *MaterializedState, ev RuntimeEvent) error {
	if state.TaskScopedHistory == nil {
		state.TaskScopedHistory = make(map[string][]HistoryItem)
	}
	if state.ActiveTasks == nil {
		state.ActiveTasks = make(map[string]*ActiveTask)
	}

	switch ev.Type {
	case EventTypeSessionCreated:
		p, ok := ev.Payload.(*SessionCreatedPayload)
		if ok && p != nil {
			state.Blueprint = p.BlueprintPayload
			if state.Blueprint != nil {
				state.EffectiveToolPolicy = &state.Blueprint.EffectiveToolPolicy
			}
		}
		state.Status = "running"

	case EventTypeTurnStarted:
		p, ok := ev.Payload.(*TurnStartedPayload)
		if ok && p != nil {
			state.CurrentTurnID = p.TurnID
		}

	case EventTypeTurnCompleted:
		state.CurrentTurnID = ""

	case EventTypePromptMaterialized:
		p, ok := ev.Payload.(*PromptMaterializedPayload)
		if ok && p != nil {
			state.LastPromptMaterialized = &PromptMaterializedSummary{
				PromptMaterializedID: p.PromptMaterializedID,
				PromptHash:           p.PromptHash,
				Seq:                  ev.Seq,
			}
		}

	case EventTypeApprovalRequested:
		p, ok := ev.Payload.(*ApprovalRequestedPayload)
		if ok && p != nil {
			state.CurrentApproval = &PendingApproval{
				ApprovalID: p.ApprovalID,
				PolicyHash: p.PolicyHash,
				ToolCallID: p.ToolCallID,
				Reason:     p.Reason,
			}
		}

	case EventTypeApprovalResolved:
		state.CurrentApproval = nil
		// 若有 cache_key，记录到 ResolvedApprovals 供跨重启查询（替代 session.ApprovalState 旧路径）。
		if p, ok := ev.Payload.(*ApprovalResolvedPayload); ok && p != nil && strings.TrimSpace(p.CacheKey) != "" {
			state.ResolvedApprovals = append(state.ResolvedApprovals, ResolvedApprovalEntry{
				CacheKey:     p.CacheKey,
				ToolName:     p.ToolName,
				DecisionType: p.DecisionType,
				Approved:     p.Approved,
				ResolvedAt:   ev.Timestamp,
			})
		}

	case EventTypePermissionsAmended:
		// EffectiveToolPolicy 将在下一次 blueprint 重新编译时更新；
		// 此处只记录 amendment 事件已发生，具体 policy 由 PolicyCompiler 重算。

	case EventTypeSessionCompleted:
		state.Status = "completed"

	case EventTypeSessionFailed:
		state.Status = "failed"

	case EventTypeTaskStarted:
		p, ok := ev.Payload.(*TaskStartedPayload)
		if ok && p != nil {
			state.ActiveTasks[p.TaskID] = &ActiveTask{
				TaskID:             p.TaskID,
				TaskStartedEventID: fmt.Sprintf("%s:%d", ev.SessionID, ev.Seq),
				TaskStartedSeq:     ev.Seq,
				PlanningItemID:     p.PlanningItemID,
				ClaimedBy:          p.ClaimedBy,
				StartedAt:          ev.Timestamp,
			}
		}

	case EventTypeTaskCompleted:
		p, ok := ev.Payload.(*TaskCompletedPayload)
		if ok && p != nil {
			e.retireTaskScopedItems(state, p.TaskID, p.PromoteScopedItems)
			delete(state.ActiveTasks, p.TaskID)
		}

	case EventTypeTaskAbandoned:
		p, ok := ev.Payload.(*TaskAbandonedPayload)
		if ok && p != nil {
			e.retireTaskScopedItems(state, p.TaskID, p.PromoteScopedItems)
			delete(state.ActiveTasks, p.TaskID)
		}

	case EventTypeRoleTransitioned:
		// RoleOverlay 切换由下一次 PromptCompiler 物化时生效。

	case EventTypePlanUpdated:
		p, ok := ev.Payload.(*PlanUpdatedPayload)
		if ok && p != nil {
			state.PlanningState = &PlanningState{
				Snapshot: p.PlanningStateSnapshot,
				LastSeq:  ev.Seq,
			}
		}

	case EventTypeBudgetLimitUpdated:
		p, ok := ev.Payload.(*BudgetLimitUpdatedPayload)
		if ok && p != nil && state.Blueprint != nil {
			state.Blueprint.ContextBudget.MainTokenBudget = int(p.MainTokenBudget)
		}

	case EventTypeSessionForked:
		p, ok := ev.Payload.(*SessionForkedPayload)
		if ok && p != nil && p.BlueprintPayload != nil {
			state.Blueprint = p.BlueprintPayload
			state.EffectiveToolPolicy = &state.Blueprint.EffectiveToolPolicy
		}
	}

	if ev.Seq > state.CurrentSeq {
		state.CurrentSeq = ev.Seq
	}
	return nil
}

// Replay 从空状态全量重放事件流，返回重建的 MaterializedState。
// 重放完成后执行不变量检查（§4.4 规则 6、§7.3 规则 11）。
func (e *ProjectionEngine) Replay(sessionID string, events []RuntimeEvent) (*MaterializedState, error) {
	state := &MaterializedState{
		SessionID:         sessionID,
		TaskScopedHistory: make(map[string][]HistoryItem),
		ActiveTasks:       make(map[string]*ActiveTask),
	}
	for _, ev := range events {
		if err := e.Apply(state, ev); err != nil {
			return nil, fmt.Errorf("replay failed at seq %d: %w", ev.Seq, err)
		}
	}
	if err := e.checkInvariants(state); err != nil {
		return nil, err
	}
	return state, nil
}

// checkInvariants 执行 replay 结束后的不变量检查（§4.4 规则 6）。
// 若存在 active task-scoped item 但对应 task 已结束，则返回错误。
func (e *ProjectionEngine) checkInvariants(state *MaterializedState) error {
	for boundTaskEventID, items := range state.TaskScopedHistory {
		// 判断该 task 是否仍在 ActiveTasks 中
		taskStillActive := false
		for _, at := range state.ActiveTasks {
			if fmt.Sprintf("%s:%d", state.SessionID, at.TaskStartedSeq) == boundTaskEventID {
				taskStillActive = true
				break
			}
		}
		if taskStillActive {
			continue
		}
		// task 已结束，检查是否有仍为 active 的 task-scoped item
		for _, item := range items {
			if item.Active {
				return fmt.Errorf(
					"invariant violation: task-scoped item %q (bound_to_task_event_id=%q) is still active but its task has ended",
					item.ItemID, boundTaskEventID,
				)
			}
		}
	}
	return nil
}

// retireTaskScopedItems 在 task 结束时执行 retire 逻辑（§5.3 promote_scoped_items 约束）。
// promote=true 的 item 提升为 persistent history；promote=false 的 item 标记 retired。
// 若 promoteItems 为空，所有该 task 下的 task-scoped item 默认 retire（不提升）。
func (e *ProjectionEngine) retireTaskScopedItems(
	state *MaterializedState,
	taskID string,
	promoteItems []PromoteScopedItem,
) {
	// 找到对应 task 的 TaskStartedEventID
	at, exists := state.ActiveTasks[taskID]
	if !exists {
		return
	}
	boundKey := fmt.Sprintf("%s:%d", state.SessionID, at.TaskStartedSeq)

	items, ok := state.TaskScopedHistory[boundKey]
	if !ok {
		return
	}

	// 建立 promote 决策索引
	promoteIndex := make(map[string]PromoteScopedItem, len(promoteItems))
	for _, pi := range promoteItems {
		promoteIndex[pi.ItemID] = pi
	}

	for i := range items {
		item := &items[i]
		if !item.Active {
			continue
		}
		decision, hasDec := promoteIndex[item.ItemID]
		if hasDec && decision.Promote {
			// 提升为 persistent history item
			promoted := HistoryItem{
				ItemID:    item.ItemID,
				Scope:     HistoryItemScopePersistent,
				Active:    true,
				Content:   HistoryContent{Text: decision.Summary},
				OriginSeq: item.OriginSeq,
			}
			state.PersistentHistory = append(state.PersistentHistory, promoted)
		}
		// 无论是否提升，task-scoped item 均标记 retired
		item.Active = false
	}
	state.TaskScopedHistory[boundKey] = items
}
