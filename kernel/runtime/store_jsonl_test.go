package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestJSONLStore(t *testing.T) *JSONLEventStore {
	t.Helper()
	store, err := NewJSONLEventStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLEventStore: %v", err)
	}
	return store
}

func TestJSONLStore_AppendAndLoad(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)

	evs := []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
		{Type: EventTypeSwarmStarted, Timestamp: time.Now(), Payload: &SwarmStartedPayload{SwarmRunID: "swarm-1", Goal: "research"}},
	}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", evs); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	loaded, err := store.LoadEvents(ctx, "sess1", 0)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("loaded %d events, want 3", len(loaded))
	}
	if loaded[0].Seq != 1 {
		t.Errorf("seq[0] = %d, want 1", loaded[0].Seq)
	}
	if loaded[2].Type != EventTypeSwarmStarted {
		t.Errorf("type[2] = %s, want %s", loaded[2].Type, EventTypeSwarmStarted)
	}
}

func TestJSONLStore_OCC_Conflict(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
	}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}

	// expectedSeq=0，实际 last_seq=1 → ErrSeqConflict
	err := store.AppendEvents(ctx, "sess1", 0, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	})
	if !errors.Is(err, ErrSeqConflict) {
		t.Errorf("expected ErrSeqConflict, got %v", err)
	}
}

func TestJSONLStore_Idempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)

	ev := RuntimeEvent{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}}
	if err := store.AppendEvents(ctx, "sess1", 0, "req-idem", []RuntimeEvent{ev}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}
	// 相同 requestID → 幂等跳过
	if err := store.AppendEvents(ctx, "sess1", 0, "req-idem", []RuntimeEvent{ev}); err != nil {
		t.Errorf("idempotent call should not fail: %v", err)
	}

	loaded, _ := store.LoadEvents(ctx, "sess1", 0)
	if len(loaded) != 1 {
		t.Errorf("loaded %d events, want 1", len(loaded))
	}
}

func TestJSONLStore_SessionEnded_RejectsAppend(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCompleted, Timestamp: time.Now(), Payload: &SessionCompletedPayload{}},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := store.AppendEvents(ctx, "sess1", 1, "req-2", []RuntimeEvent{
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	})
	if !errors.Is(err, ErrSessionEnded) {
		t.Errorf("expected ErrSessionEnded, got %v", err)
	}
}

func TestJSONLStore_LoadSessionView_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)
	_, err := store.LoadSessionView(ctx, "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestJSONLStore_ExportImport_JSONL(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)

	if err := store.AppendEvents(ctx, "sess1", 0, "req-1", []RuntimeEvent{
		{Type: EventTypeSessionCreated, Timestamp: time.Now(), Payload: &SessionCreatedPayload{}},
		{Type: EventTypeTurnStarted, Timestamp: time.Now(), Payload: &TurnStartedPayload{TurnID: "t1"}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	data, err := store.Export(ctx, "sess1", ExportFormatJSONL)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

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

func TestJSONLStore_SubscribeEvents_NotSupported(t *testing.T) {
	ctx := context.Background()
	store := newTestJSONLStore(t)
	_, err := store.SubscribeEvents(ctx, "sess1", 0)
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}
