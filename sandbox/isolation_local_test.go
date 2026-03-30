package sandbox

import (
	"context"
	"testing"
)

func TestLocalWorkspaceIsolation_AcquireRelease(t *testing.T) {
	iso, err := NewLocalWorkspaceIsolation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	lease1, err := iso.Acquire(ctx, "task:1")
	if err != nil {
		t.Fatal(err)
	}
	if lease1.WorkspaceID == "" || lease1.Workspace == nil || lease1.Executor == nil {
		t.Fatalf("invalid lease: %+v", lease1)
	}

	lease2, err := iso.Acquire(ctx, "task:1")
	if err != nil {
		t.Fatal(err)
	}
	if lease2.WorkspaceID != lease1.WorkspaceID {
		t.Fatalf("expected same workspace id, got %q and %q", lease1.WorkspaceID, lease2.WorkspaceID)
	}

	if err := iso.Release(ctx, lease1.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	lease3, err := iso.Acquire(ctx, "task:1")
	if err != nil {
		t.Fatal(err)
	}
	if lease3.WorkspaceID == "" {
		t.Fatal("expected workspace id after reacquire")
	}
}

func TestLocalWorkspaceIsolation_RestoresLeaseJournal(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	iso1, err := NewLocalWorkspaceIsolation(root)
	if err != nil {
		t.Fatal(err)
	}
	lease1, err := iso1.Acquire(ctx, "task:recover")
	if err != nil {
		t.Fatal(err)
	}
	if lease1.Recovered {
		t.Fatal("first acquire should not be marked recovered")
	}

	iso2, err := NewLocalWorkspaceIsolation(root)
	if err != nil {
		t.Fatal(err)
	}
	lease2, err := iso2.Acquire(ctx, "task:recover")
	if err != nil {
		t.Fatal(err)
	}
	if lease2.WorkspaceID != lease1.WorkspaceID {
		t.Fatalf("expected restored workspace id %q, got %q", lease1.WorkspaceID, lease2.WorkspaceID)
	}
	if !lease2.Recovered {
		t.Fatal("expected recovered lease after restart")
	}
	if lease2.TaskID != "task:recover" {
		t.Fatalf("expected task id task:recover, got %q", lease2.TaskID)
	}
}
