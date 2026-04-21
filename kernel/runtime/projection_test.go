package runtime

import (
	"fmt"
	"testing"
	"time"
)

// ─────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────

func makeEv(sessionID string, seq int64, evType EventType, payload any) RuntimeEvent {
	return RuntimeEvent{
		SessionID: sessionID,
		Seq:       seq,
		Timestamp: time.Now(),
		Type:      evType,
		Payload:   payload,
	}
}

// boundKey 返回 task-scoped items 使用的 map key，与 projection.go 保持一致。
func boundKey(sessionID string, taskStartedSeq int64) string {
	return fmt.Sprintf("%s:%d", sessionID, taskStartedSeq)
}

func newState(sessionID string) *MaterializedState {
	return &MaterializedState{
		SessionID:         sessionID,
		TaskScopedHistory: make(map[string][]HistoryItem),
		ActiveTasks:       make(map[string]*ActiveTask),
	}
}

// ─────────────────────────────────────────────
// SessionCreated
// ─────────────────────────────────────────────

func TestApply_SessionCreated_SetsStatusAndBlueprint(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	bp := &SessionBlueprint{
		Identity: BlueprintIdentity{SessionID: "sess1"},
		EffectiveToolPolicy: EffectiveToolPolicy{
			PolicyHash: "abc123",
			TrustLevel: "medium",
		},
	}
	ev := makeEv("sess1", 1, EventTypeSessionCreated, &SessionCreatedPayload{
		BlueprintPayload: bp,
		TriggerSource:    "interactive",
	})

	if err := eng.Apply(state, ev); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Status != "running" {
		t.Errorf("status = %q, want %q", state.Status, "running")
	}
	if state.Blueprint == nil {
		t.Fatal("Blueprint is nil")
	}
	if state.Blueprint.Identity.SessionID != "sess1" {
		t.Errorf("blueprint session_id = %q", state.Blueprint.Identity.SessionID)
	}
	if state.EffectiveToolPolicy == nil || state.EffectiveToolPolicy.PolicyHash != "abc123" {
		t.Error("EffectiveToolPolicy not set from blueprint")
	}
}

// ─────────────────────────────────────────────
// TurnBoundary
// ─────────────────────────────────────────────

func TestApply_TurnBoundary(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	if err := eng.Apply(state, makeEv("sess1", 1, EventTypeTurnStarted, &TurnStartedPayload{TurnID: "turn-1"})); err != nil {
		t.Fatal(err)
	}
	if state.CurrentTurnID != "turn-1" {
		t.Errorf("CurrentTurnID = %q, want %q", state.CurrentTurnID, "turn-1")
	}

	if err := eng.Apply(state, makeEv("sess1", 2, EventTypeTurnCompleted, &TurnCompletedPayload{
		TurnID:  "turn-1",
		Outcome: TurnOutcomeCompleted,
	})); err != nil {
		t.Fatal(err)
	}
	if state.CurrentTurnID != "" {
		t.Errorf("CurrentTurnID should be cleared after TurnCompleted, got %q", state.CurrentTurnID)
	}
}

// ─────────────────────────────────────────────
// TaskStarted → ActiveTasks
// ─────────────────────────────────────────────

func TestApply_TaskStarted_AddsToActiveTasks(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	ev := makeEv("sess1", 3, EventTypeTaskStarted, &TaskStartedPayload{
		TaskID:         "task-A",
		PlanningItemID: "plan-1",
		ClaimedBy:      "worker-1",
	})
	if err := eng.Apply(state, ev); err != nil {
		t.Fatal(err)
	}
	at, ok := state.ActiveTasks["task-A"]
	if !ok {
		t.Fatal("task-A not in ActiveTasks")
	}
	if at.TaskStartedSeq != 3 {
		t.Errorf("TaskStartedSeq = %d, want 3", at.TaskStartedSeq)
	}
	if at.ClaimedBy != "worker-1" {
		t.Errorf("ClaimedBy = %q", at.ClaimedBy)
	}
}

// ─────────────────────────────────────────────
// TaskCompleted → retire task-scoped items（无 promote）
// ─────────────────────────────────────────────

func TestApply_TaskCompleted_RetiresScopedItems(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	// 1. task_started（seq=3）
	startEv := makeEv("sess1", 3, EventTypeTaskStarted, &TaskStartedPayload{TaskID: "task-A"})
	if err := eng.Apply(state, startEv); err != nil {
		t.Fatal(err)
	}

	// 2. 手动添加两个 task-scoped items
	key := boundKey("sess1", 3)
	state.TaskScopedHistory[key] = []HistoryItem{
		{ItemID: "item-1", Scope: HistoryItemScopeTaskScoped, Active: true, BoundToTaskEventID: key, Content: HistoryContent{Text: "ctx A"}},
		{ItemID: "item-2", Scope: HistoryItemScopeTaskScoped, Active: true, BoundToTaskEventID: key, Content: HistoryContent{Text: "ctx B"}},
	}

	// 3. task_completed（无 promote）
	compEv := makeEv("sess1", 5, EventTypeTaskCompleted, &TaskCompletedPayload{
		TaskID: "task-A",
	})
	if err := eng.Apply(state, compEv); err != nil {
		t.Fatal(err)
	}

	// task 应从 ActiveTasks 移除
	if _, ok := state.ActiveTasks["task-A"]; ok {
		t.Error("task-A should be removed from ActiveTasks")
	}
	// 所有 items 应被 retire（Active=false）
	for _, item := range state.TaskScopedHistory[key] {
		if item.Active {
			t.Errorf("item %q should be retired (Active=false)", item.ItemID)
		}
	}
	// PersistentHistory 应为空
	if len(state.PersistentHistory) != 0 {
		t.Errorf("PersistentHistory should be empty, got %d items", len(state.PersistentHistory))
	}
}

// ─────────────────────────────────────────────
// TaskCompleted → promote 部分 items
// ─────────────────────────────────────────────

func TestApply_TaskCompleted_PromotesScopedItems(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	startEv := makeEv("sess1", 4, EventTypeTaskStarted, &TaskStartedPayload{TaskID: "task-B"})
	if err := eng.Apply(state, startEv); err != nil {
		t.Fatal(err)
	}

	key := boundKey("sess1", 4)
	state.TaskScopedHistory[key] = []HistoryItem{
		{ItemID: "item-keep", Scope: HistoryItemScopeTaskScoped, Active: true, Content: HistoryContent{Text: "important"}},
		{ItemID: "item-drop", Scope: HistoryItemScopeTaskScoped, Active: true, Content: HistoryContent{Text: "transient"}},
	}

	compEv := makeEv("sess1", 6, EventTypeTaskCompleted, &TaskCompletedPayload{
		TaskID: "task-B",
		PromoteScopedItems: []PromoteScopedItem{
			{ItemID: "item-keep", Promote: true, Summary: "task B completed: important outcome"},
			{ItemID: "item-drop", Promote: false},
		},
	})
	if err := eng.Apply(state, compEv); err != nil {
		t.Fatal(err)
	}

	if len(state.PersistentHistory) != 1 {
		t.Fatalf("PersistentHistory len = %d, want 1", len(state.PersistentHistory))
	}
	promoted := state.PersistentHistory[0]
	if promoted.ItemID != "item-keep" {
		t.Errorf("promoted item ID = %q, want %q", promoted.ItemID, "item-keep")
	}
	if promoted.Scope != HistoryItemScopePersistent {
		t.Errorf("promoted item scope = %q, want persistent", promoted.Scope)
	}
	if promoted.Content.Text != "task B completed: important outcome" {
		t.Errorf("promoted summary = %q", promoted.Content.Text)
	}
	if promoted.Active != true {
		t.Error("promoted item should be Active=true")
	}

	// 原始 task-scoped items 均已 retire
	for _, item := range state.TaskScopedHistory[key] {
		if item.Active {
			t.Errorf("task-scoped item %q should be retired", item.ItemID)
		}
	}
}

// ─────────────────────────────────────────────
// TaskAbandoned → retire（与 Completed 相同逻辑）
// ─────────────────────────────────────────────

func TestApply_TaskAbandoned_RetiresScopedItems(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	if err := eng.Apply(state, makeEv("sess1", 2, EventTypeTaskStarted, &TaskStartedPayload{TaskID: "task-C"})); err != nil {
		t.Fatal(err)
	}
	key := boundKey("sess1", 2)
	state.TaskScopedHistory[key] = []HistoryItem{
		{ItemID: "item-x", Scope: HistoryItemScopeTaskScoped, Active: true},
	}

	if err := eng.Apply(state, makeEv("sess1", 3, EventTypeTaskAbandoned, &TaskAbandonedPayload{
		TaskID: "task-C",
		Reason: "timeout",
	})); err != nil {
		t.Fatal(err)
	}

	if _, ok := state.ActiveTasks["task-C"]; ok {
		t.Error("task-C should be removed from ActiveTasks")
	}
	for _, item := range state.TaskScopedHistory[key] {
		if item.Active {
			t.Errorf("item %q should be retired after task_abandoned", item.ItemID)
		}
	}
}

// ─────────────────────────────────────────────
// ApprovalRoundtrip
// ─────────────────────────────────────────────

func TestApply_ApprovalRoundtrip(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	if err := eng.Apply(state, makeEv("sess1", 1, EventTypeApprovalRequested, &ApprovalRequestedPayload{
		ApprovalID: "appr-1",
		PolicyHash: "ph1",
		ToolCallID: "tc-1",
		Reason:     "risky op",
	})); err != nil {
		t.Fatal(err)
	}
	if state.CurrentApproval == nil {
		t.Fatal("CurrentApproval should be set")
	}
	if state.CurrentApproval.ApprovalID != "appr-1" {
		t.Errorf("ApprovalID = %q", state.CurrentApproval.ApprovalID)
	}

	if err := eng.Apply(state, makeEv("sess1", 2, EventTypeApprovalResolved, &ApprovalResolvedPayload{
		ApprovalID:   "appr-1",
		ResolverType: ResolverTypeHuman,
		Approved:     true,
	})); err != nil {
		t.Fatal(err)
	}
	if state.CurrentApproval != nil {
		t.Error("CurrentApproval should be cleared after ApprovalResolved")
	}
}

// ─────────────────────────────────────────────
// SessionTerminated
// ─────────────────────────────────────────────

func TestApply_SessionCompleted_SetsStatus(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")
	state.Status = "running"

	if err := eng.Apply(state, makeEv("sess1", 10, EventTypeSessionCompleted, &SessionCompletedPayload{Summary: "done"})); err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" {
		t.Errorf("status = %q, want completed", state.Status)
	}
}

func TestApply_SessionFailed_SetsStatus(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")
	state.Status = "running"

	if err := eng.Apply(state, makeEv("sess1", 11, EventTypeSessionFailed, &SessionFailedPayload{
		ErrorKind:    "budget_exhausted",
		ErrorMessage: "out of tokens",
		LastSeq:      10,
	})); err != nil {
		t.Fatal(err)
	}
	if state.Status != "failed" {
		t.Errorf("status = %q, want failed", state.Status)
	}
}

// ─────────────────────────────────────────────
// PlanUpdated
// ─────────────────────────────────────────────

func TestApply_PlanUpdated(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	snapshot := map[string]any{"steps": []string{"a", "b", "c"}}
	if err := eng.Apply(state, makeEv("sess1", 5, EventTypePlanUpdated, &PlanUpdatedPayload{
		PlanningStateSnapshot: snapshot,
	})); err != nil {
		t.Fatal(err)
	}
	if state.PlanningState == nil {
		t.Fatal("PlanningState should be set")
	}
	if state.PlanningState.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", state.PlanningState.LastSeq)
	}
}

// ─────────────────────────────────────────────
// CurrentSeq 单调递增
// ─────────────────────────────────────────────

func TestApply_CurrentSeq_MonotonicallyIncreases(t *testing.T) {
	eng := NewProjectionEngine()
	state := newState("sess1")

	evs := []RuntimeEvent{
		makeEv("sess1", 1, EventTypeSessionCreated, &SessionCreatedPayload{}),
		makeEv("sess1", 2, EventTypeTurnStarted, &TurnStartedPayload{TurnID: "t1"}),
		makeEv("sess1", 3, EventTypeTurnCompleted, &TurnCompletedPayload{TurnID: "t1", Outcome: TurnOutcomeCompleted}),
	}
	for _, ev := range evs {
		if err := eng.Apply(state, ev); err != nil {
			t.Fatalf("Apply seq=%d: %v", ev.Seq, err)
		}
	}
	if state.CurrentSeq != 3 {
		t.Errorf("CurrentSeq = %d, want 3", state.CurrentSeq)
	}
}

// ─────────────────────────────────────────────
// Replay：全量重建一致性
// ─────────────────────────────────────────────

func TestReplay_FullSequence(t *testing.T) {
	eng := NewProjectionEngine()

	events := []RuntimeEvent{
		makeEv("sess1", 1, EventTypeSessionCreated, &SessionCreatedPayload{
			BlueprintPayload: &SessionBlueprint{Identity: BlueprintIdentity{SessionID: "sess1"}},
		}),
		makeEv("sess1", 2, EventTypeTurnStarted, &TurnStartedPayload{TurnID: "t1"}),
		makeEv("sess1", 3, EventTypeTaskStarted, &TaskStartedPayload{TaskID: "task-A"}),
		makeEv("sess1", 4, EventTypeTaskCompleted, &TaskCompletedPayload{TaskID: "task-A"}),
		makeEv("sess1", 5, EventTypeTurnCompleted, &TurnCompletedPayload{TurnID: "t1", Outcome: TurnOutcomeCompleted}),
		makeEv("sess1", 6, EventTypeSessionCompleted, &SessionCompletedPayload{Summary: "all done"}),
	}

	state, err := eng.Replay("sess1", events)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if state.Status != "completed" {
		t.Errorf("status = %q, want completed", state.Status)
	}
	if state.CurrentSeq != 6 {
		t.Errorf("CurrentSeq = %d, want 6", state.CurrentSeq)
	}
	if state.CurrentTurnID != "" {
		t.Errorf("CurrentTurnID should be empty after turn_completed, got %q", state.CurrentTurnID)
	}
	if len(state.ActiveTasks) != 0 {
		t.Errorf("ActiveTasks should be empty after task_completed")
	}
}

// ─────────────────────────────────────────────
// 不变量检查：task 已结束但仍有 active task-scoped item
// ─────────────────────────────────────────────

func TestCheckInvariants_ViolationDetected(t *testing.T) {
	eng := NewProjectionEngine()
	// 构造一个损坏的 state：task 不在 ActiveTasks，但 TaskScopedHistory 中有 active item
	state := &MaterializedState{
		SessionID:   "sess1",
		ActiveTasks: map[string]*ActiveTask{}, // task 已不在
		TaskScopedHistory: map[string][]HistoryItem{
			"sess1:5": {
				{ItemID: "orphan", Scope: HistoryItemScopeTaskScoped, Active: true},
			},
		},
	}
	err := eng.checkInvariants(state)
	if err == nil {
		t.Error("expected invariant violation error, got nil")
	}
}

func TestCheckInvariants_NoViolation_TaskStillActive(t *testing.T) {
	eng := NewProjectionEngine()
	state := &MaterializedState{
		SessionID: "sess1",
		ActiveTasks: map[string]*ActiveTask{
			"task-A": {TaskID: "task-A", TaskStartedSeq: 5},
		},
		TaskScopedHistory: map[string][]HistoryItem{
			"sess1:5": {
				{ItemID: "item-1", Scope: HistoryItemScopeTaskScoped, Active: true},
			},
		},
	}
	if err := eng.checkInvariants(state); err != nil {
		t.Errorf("unexpected invariant error: %v", err)
	}
}

func TestCheckInvariants_NoViolation_ItemAlreadyRetired(t *testing.T) {
	eng := NewProjectionEngine()
	state := &MaterializedState{
		SessionID:   "sess1",
		ActiveTasks: map[string]*ActiveTask{}, // task 已结束
		TaskScopedHistory: map[string][]HistoryItem{
			"sess1:5": {
				{ItemID: "item-1", Scope: HistoryItemScopeTaskScoped, Active: false}, // 已 retire
			},
		},
	}
	if err := eng.checkInvariants(state); err != nil {
		t.Errorf("unexpected invariant error: %v", err)
	}
}
