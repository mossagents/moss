package checkpoint

import (
	"context"
	"testing"
)

func TestFileCheckpointStoreCreateLoadListFindBySession(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	created, err := store.Create(context.Background(), CheckpointCreateRequest{
		SessionID:          "sess-1",
		WorktreeSnapshotID: "snapshot-1",
		PatchIDs:           []string{"patch-1", "patch-2"},
		Note:               "checkpoint note",
		Metadata:           map[string]any{"kind": "manual"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected checkpoint id")
	}
	if created.Lineage[0].Kind != CheckpointLineageSession {
		t.Fatalf("expected default lineage session, got %+v", created.Lineage)
	}

	loaded, err := store.Load(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.WorktreeSnapshotID != "snapshot-1" {
		t.Fatalf("worktree snapshot = %q", loaded.WorktreeSnapshotID)
	}
	if len(loaded.PatchIDs) != 2 {
		t.Fatalf("patch ids = %v", loaded.PatchIDs)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("checkpoints = %d, want 1", len(items))
	}

	bySession, err := store.FindBySession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("FindBySession: %v", err)
	}
	if len(bySession) != 1 || bySession[0].ID != created.ID {
		t.Fatalf("unexpected checkpoints by session: %+v", bySession)
	}
}

func TestFileCheckpointStoreLoadMissing(t *testing.T) {
	store, err := NewFileCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	if _, err := store.Load(context.Background(), "missing"); err != ErrCheckpointNotFound {
		t.Fatalf("expected ErrCheckpointNotFound, got %v", err)
	}
}
