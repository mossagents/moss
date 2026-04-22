package kernel

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/io"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	kt "github.com/mossagents/moss/kernel/testing"
)

// ────────────────────────────────────────────────────────────────────
// 阶段 2 测试：resume / fork / session 终态 reader API
// ────────────────────────────────────────────────────────────────────

func TestLoadRuntimeSession_NoStore(t *testing.T) {
	k := New(WithLLM(&kt.MockLLM{}), WithUserIO(&io.NoOpIO{}))
	_, err := k.LoadRuntimeSession(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected error when EventStore not configured")
	}
}

func TestLoadRuntimeSession_HappyPath(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	// 先建一个 session
	bp, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}

	state, err := k.LoadRuntimeSession(context.Background(), bp.Identity.SessionID)
	if err != nil {
		t.Fatalf("LoadRuntimeSession: %v", err)
	}
	if state == nil || state.SessionID != bp.Identity.SessionID {
		t.Error("loaded state should match created session")
	}
	if state.Blueprint == nil {
		t.Error("loaded state should have blueprint")
	}
}

func TestListRuntimeResumeCandidates_HappyPath(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	// 建两个 session
	for range 2 {
		if _, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{}); err != nil {
			t.Fatalf("StartRuntimeSession: %v", err)
		}
	}

	candidates, err := k.ListRuntimeResumeCandidates(context.Background(), kruntime.ResumeCandidateFilter{})
	if err != nil {
		t.Fatalf("ListRuntimeResumeCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(candidates))
	}
}

func TestResumeRuntimeSession_HappyPath(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, _ := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "read-only",
	})

	resumed, err := k.ResumeRuntimeSession(context.Background(), bp.Identity.SessionID)
	if err != nil {
		t.Fatalf("ResumeRuntimeSession: %v", err)
	}
	if resumed.Identity.SessionID != bp.Identity.SessionID {
		t.Error("resumed blueprint should match original session")
	}
}

func TestRecordBudgetLimitUpdated_PersistsAcrossResume(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, _ := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
	})

	updated, err := k.RecordBudgetLimitUpdated(context.Background(), bp, 240000, "token_overrun_continue")
	if err != nil {
		t.Fatalf("RecordBudgetLimitUpdated: %v", err)
	}
	if updated.ContextBudget.MainTokenBudget != 240000 {
		t.Fatalf("updated main token budget = %d, want 240000", updated.ContextBudget.MainTokenBudget)
	}

	resumed, err := k.ResumeRuntimeSession(context.Background(), bp.Identity.SessionID)
	if err != nil {
		t.Fatalf("ResumeRuntimeSession: %v", err)
	}
	if resumed.ContextBudget.MainTokenBudget != 240000 {
		t.Fatalf("resumed main token budget = %d, want 240000", resumed.ContextBudget.MainTokenBudget)
	}
}

func TestResumeRuntimeSession_EndedSession(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, _ := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{})
	if err := k.CompleteRuntimeSession(context.Background(), bp.Identity.SessionID, "done"); err != nil {
		t.Fatalf("CompleteRuntimeSession: %v", err)
	}

	_, err := k.ResumeRuntimeSession(context.Background(), bp.Identity.SessionID)
	if err == nil {
		t.Fatal("expected error when resuming completed session")
	}
}

func TestForkRuntimeSession_HappyPath(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	// 建源 session
	srcBP, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}

	// Fork 出子 session
	childBP, err := k.ForkRuntimeSession(context.Background(), srcBP.Identity.SessionID, kruntime.RuntimeRequest{
		PermissionProfile: "read-only",
	})
	if err != nil {
		t.Fatalf("ForkRuntimeSession: %v", err)
	}

	// 子 session 应有新 ID
	if childBP.Identity.SessionID == srcBP.Identity.SessionID {
		t.Error("forked session should have a different session ID")
	}

	// 源 session 应有 session_forked 事件
	srcEvents, err := store.LoadEvents(context.Background(), srcBP.Identity.SessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents source: %v", err)
	}
	hasFork := false
	for _, ev := range srcEvents {
		if ev.Type == kruntime.EventTypeSessionForked {
			hasFork = true
			break
		}
	}
	if !hasFork {
		t.Error("source session should have session_forked event")
	}

	// 子 session 应有 session_created 事件（含 parent_session_id）
	childEvents, err := store.LoadEvents(context.Background(), childBP.Identity.SessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents child: %v", err)
	}
	if len(childEvents) != 1 || childEvents[0].Type != kruntime.EventTypeSessionCreated {
		t.Error("child session should have session_created event")
	}
	payload, ok := childEvents[0].Payload.(*kruntime.SessionCreatedPayload)
	if !ok {
		t.Fatalf("session_created payload type mismatch, got %T", childEvents[0].Payload)
	}
	if payload.ParentSessionID != srcBP.Identity.SessionID {
		t.Errorf("child session_created should carry parent_session_id=%s, got=%s",
			srcBP.Identity.SessionID, payload.ParentSessionID)
	}
	if payload.TriggerSource != "fork" {
		t.Errorf("expected trigger_source=fork, got=%s", payload.TriggerSource)
	}
}

func TestCompleteRuntimeSession(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, _ := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{})
	if err := k.CompleteRuntimeSession(context.Background(), bp.Identity.SessionID, "all done"); err != nil {
		t.Fatalf("CompleteRuntimeSession: %v", err)
	}

	events, _ := store.LoadEvents(context.Background(), bp.Identity.SessionID, 0)
	last := events[len(events)-1]
	if last.Type != kruntime.EventTypeSessionCompleted {
		t.Errorf("expected session_completed, got %s", last.Type)
	}
}

func TestFailRuntimeSession(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, _ := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{})
	if err := k.FailRuntimeSession(context.Background(), bp.Identity.SessionID, "panic", "unexpected error"); err != nil {
		t.Fatalf("FailRuntimeSession: %v", err)
	}

	events, _ := store.LoadEvents(context.Background(), bp.Identity.SessionID, 0)
	last := events[len(events)-1]
	if last.Type != kruntime.EventTypeSessionFailed {
		t.Errorf("expected session_failed, got %s", last.Type)
	}
}

// ────────────────────────────────────────────────────────────────────
// 阶段 5 测试：Export / Import
// ────────────────────────────────────────────────────────────────────

func TestExportRuntimeSession_NoStore(t *testing.T) {
	k := New()
	_, err := k.ExportRuntimeSession(context.Background(), "any", kruntime.ExportFormatJSONL)
	if err == nil {
		t.Fatal("expected error when EventStore not configured")
	}
}

func TestExportImportRuntimeSession_RoundTrip(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	k := New(WithEventStore(store))

	bp, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}

	// Export
	data, err := k.ExportRuntimeSession(context.Background(), bp.Identity.SessionID, kruntime.ExportFormatJSONL)
	if err != nil {
		t.Fatalf("ExportRuntimeSession: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("exported data should not be empty")
	}

	// Import 到新 store 的新 session ID（JSONL store 用于测试）
	store2, _ := kruntime.NewJSONLEventStore(t.TempDir() + "/events.jsonl")
	k2 := New(WithEventStore(store2))
	newSessionID := "imported-session-001"
	if err := k2.ImportRuntimeSession(context.Background(), newSessionID, data, kruntime.ExportFormatJSONL); err != nil {
		t.Fatalf("ImportRuntimeSession: %v", err)
	}

	// 验证导入后的事件存在
	events, err := store2.LoadEvents(context.Background(), newSessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents after import: %v", err)
	}
	if len(events) == 0 {
		t.Error("imported session should have events")
	}
}
