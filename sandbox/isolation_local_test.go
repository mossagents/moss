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

