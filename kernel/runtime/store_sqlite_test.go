package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// newTestSQLiteStore 在临时目录创建 SQLiteEventStore，测试结束自动清理。
func newTestSQLiteStore(t *testing.T) *SQLiteEventStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteEventStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteEventStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func sessionCreatedEvent(sessionID string, seq int64) RuntimeEvent {
	return RuntimeEvent{
		SessionID: sessionID,
		Seq:       seq,
		Timestamp: time.Now(),
		Type:      EventTypeSessionCreated,
		RequestID: "req-" + sessionID,
		Payload: &SessionCreatedPayload{
			BlueprintPayload: &SessionBlueprint{
				Identity: BlueprintIdentity{SessionID: sessionID},
			},
		},
	}
}

// ─────────────────────────────────────────────
// 基本 Append + Load
// ─────────────────────────────────────────────

func TestSQLiteStore_AppendAndLoad(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	evs := []RuntimeEvent{
		sessionCreatedEvent("sess1", 0), // seq 由 store 分配，传入的 Seq 字段被覆盖
		{SessionID: "sess1", Type: EventTypeTurnStarted, Timestamp: time.Now(),
			Payload: &TurnStartedPayload{TurnID: "t1"}},
	}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", evs); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	loaded, err := store.LoadEvents(ctx, "sess1", 0)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("loaded %d events, want 2", len(loaded))
	}
	if loaded[0].Seq != 1 {
		t.Errorf("first event seq = %d, want 1", loaded[0].Seq)
	}
	if loaded[1].Seq != 2 {
		t.Errorf("second event seq = %d, want 2", loaded[1].Seq)
	}
}

// ─────────────────────────────────────────────
// afterSeq 过滤
// ─────────────────────────────────────────────

func TestSQLiteStore_LoadEvents_AfterSeq(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	evs := []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
		{Type: EventTypeTurnCompleted, Timestamp: time.Now(), Payload: &TurnCompletedPayload{TurnID: "t1", Outcome: TurnOutcomeCompleted}},
	}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-x", evs); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// 只取 seq > 1 的事件
	loaded, err := store.LoadEvents(ctx, "sess1", 1)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("loaded %d events, want 2", len(loaded))
	}
	if loaded[0].Seq != 2 {
		t.Errorf("first event seq = %d, want 2", loaded[0].Seq)
	}
}

// ─────────────────────────────────────────────
// Optimistic Concurrency Control
// ─────────────────────────────────────────────

func TestSQLiteStore_OCC_Conflict(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	// 先追加 1 条，last_seq=1
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
	}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}

	// 用错误的 expectedSeq=0（实际为 1）→ 应返回 ErrSeqConflict
	err := store.AppendEvents(ctx, "sess1", 0, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	})
	if !errors.Is(err, ErrSeqConflict) {
		t.Errorf("expected ErrSeqConflict, got %v", err)
	}
}

func TestSQLiteStore_OCC_CorrectSeq(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
	}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}

	// 用正确的 expectedSeq=1
	if err := store.AppendEvents(ctx, "sess1", 1, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	}); err != nil {
		t.Errorf("second AppendEvents: %v", err)
	}

	loaded, _ := store.LoadEvents(ctx, "sess1", 0)
	if len(loaded) != 2 {
		t.Errorf("loaded %d events, want 2", len(loaded))
	}
}

// ─────────────────────────────────────────────
// 幂等：同一 requestID 只写入一次
// ─────────────────────────────────────────────

func TestSQLiteStore_Idempotent_SameRequestID(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	ev := RuntimeEvent{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}}

	// 第一次写入
	if err := store.AppendEvents(ctx, "sess1", 0, "req-idempotent", []RuntimeEvent{ev}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}
	// 第二次相同 requestID → 幂等跳过，不报错
	if err := store.AppendEvents(ctx, "sess1", 0, "req-idempotent", []RuntimeEvent{ev}); err != nil {
		t.Errorf("idempotent AppendEvents should not fail: %v", err)
	}

	// 只有 1 条事件
	loaded, _ := store.LoadEvents(ctx, "sess1", 0)
	if len(loaded) != 1 {
		t.Errorf("loaded %d events, want 1 (idempotent)", len(loaded))
	}
}

// ─────────────────────────────────────────────
// session 终态保护
// ─────────────────────────────────────────────

func TestSQLiteStore_SessionEnded_RejectsAppend(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	// 追加 session_completed
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeSessionCompleted, Timestamp: time.Now(), Payload: &SessionCompletedPayload{}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// 再次追加 → 应返回 ErrSessionEnded
	err := store.AppendEvents(ctx, "sess1", 2, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	})
	if !errors.Is(err, ErrSessionEnded) {
		t.Errorf("expected ErrSessionEnded, got %v", err)
	}
}

func TestSQLiteStore_SessionFailed_RejectsAppend(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionFailed, Timestamp: time.Now(), Payload: &SessionFailedPayload{
			ErrorKind: "crash", LastSeq: 0,
		}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	err := store.AppendEvents(ctx, "sess1", 1, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	})
	if !errors.Is(err, ErrSessionEnded) {
		t.Errorf("expected ErrSessionEnded, got %v", err)
	}
}

// ─────────────────────────────────────────────
// LoadSessionView
// ─────────────────────────────────────────────

func TestSQLiteStore_LoadSessionView(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{
			BlueprintPayload: &SessionBlueprint{Identity: BlueprintIdentity{SessionID: "sess1"}},
		}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	state, err := store.LoadSessionView(ctx, "sess1")
	if err != nil {
		t.Fatalf("LoadSessionView: %v", err)
	}
	if state.SessionID != "sess1" {
		t.Errorf("SessionID = %q", state.SessionID)
	}
	if state.Status != "running" {
		t.Errorf("status = %q, want running", state.Status)
	}
	if state.CurrentTurnID != "t1" {
		t.Errorf("CurrentTurnID = %q, want t1", state.CurrentTurnID)
	}
}

// ─────────────────────────────────────────────
// LoadSessionView → session 不存在
// ─────────────────────────────────────────────

func TestSQLiteStore_LoadSessionView_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	_, err := store.LoadSessionView(ctx, "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────
// RebuildProjections
// ─────────────────────────────────────────────

func TestSQLiteStore_RebuildProjections(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if err := store.RebuildProjections(ctx, "sess1"); err != nil {
		t.Errorf("RebuildProjections: %v", err)
	}
}

// ─────────────────────────────────────────────
// ListResumeCandidates
// ─────────────────────────────────────────────

func TestSQLiteStore_ListResumeCandidates(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	// sess1 → running（可恢复）
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
	}); err != nil {
		t.Fatalf("setup sess1: %v", err)
	}
	// sess2 → completed（不可恢复）
	if err := store.AppendEvents(ctx, "sess2", 0, "req-2", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeSessionCompleted, Timestamp: time.Now(), Payload: &SessionCompletedPayload{}},
	}); err != nil {
		t.Fatalf("setup sess2: %v", err)
	}

	candidates, err := store.ListResumeCandidates(ctx, ResumeCandidateFilter{})
	if err != nil {
		t.Fatalf("ListResumeCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Errorf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].SessionID != "sess1" {
		t.Errorf("candidate session_id = %q, want sess1", candidates[0].SessionID)
	}
}

// ─────────────────────────────────────────────
// Export / Import 往返
// ─────────────────────────────────────────────

func TestSQLiteStore_ExportImport_JSONL(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	original := []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", original); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	data, err := store.Export(ctx, "sess1", ExportFormatJSONL)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("exported data is empty")
	}

	// 导入到新 session
	if err := store.Import(ctx, "sess2", data, ExportFormatJSONL); err != nil {
		t.Fatalf("Import: %v", err)
	}

	imported, err := store.LoadEvents(ctx, "sess2", 0)
	if err != nil {
		t.Fatalf("LoadEvents after import: %v", err)
	}
	if len(imported) != 2 {
		t.Errorf("imported %d events, want 2", len(imported))
	}
}

// ─────────────────────────────────────────────
// SubscribeEvents → ErrNotSupported
// ─────────────────────────────────────────────

func TestSQLiteStore_SubscribeEvents_NotSupported(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)
	_, err := store.SubscribeEvents(ctx, "sess1", 0)
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}

// ─────────────────────────────────────────────
// 多事件批量原子追加（§10）
// ─────────────────────────────────────────────

func TestSQLiteStore_BatchAppend_SeqContinuous(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	batch := []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
		{Type: EventTypeTaskStarted, Timestamp: time.Now(), Payload: &TaskStartedPayload{TaskID: "task-A"}},
	}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-batch", batch); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	loaded, err := store.LoadEvents(ctx, "sess1", 0)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded %d events, want 3", len(loaded))
	}
	for i, ev := range loaded {
		if ev.Seq != int64(i+1) {
			t.Errorf("event[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
}
