package sandbox

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestGitWorktreeSnapshotStore_CreateLoadList(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "one\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "two\n")
	patch := gitOutput(t, repo, "diff")
	runGit(t, repo, "checkout", "--", "tracked.txt")

	applier := NewGitPatchApply(repo)
	applied, err := applier.Apply(context.Background(), port.PatchApplyRequest{
		Patch:  patch,
		Source: port.PatchSourceLLM,
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewGitWorktreeSnapshotStore(repo)
	snapshot, err := store.Create(context.Background(), port.WorktreeSnapshotRequest{
		Note: "before review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != port.WorktreeSnapshotGhostState {
		t.Fatalf("unexpected snapshot mode %q", snapshot.Mode)
	}
	if len(snapshot.Patches) != 1 || snapshot.Patches[0].PatchID != applied.PatchID {
		t.Fatalf("expected one patch ref, got %+v", snapshot.Patches)
	}

	loaded, err := store.Load(context.Background(), snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Note != "before review" || loaded.ID != snapshot.ID {
		t.Fatalf("unexpected loaded snapshot %+v", loaded)
	}

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != snapshot.ID {
		t.Fatalf("unexpected snapshot list %+v", list)
	}
}

func TestGitWorktreeSnapshotStore_Unavailable(t *testing.T) {
	store := NewGitWorktreeSnapshotStore(t.TempDir())
	_, err := store.Create(context.Background(), port.WorktreeSnapshotRequest{})
	if err != port.ErrWorktreeSnapshotUnavailable {
		t.Fatalf("expected ErrWorktreeSnapshotUnavailable, got %v", err)
	}
}
