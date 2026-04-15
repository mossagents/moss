package sandbox

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/workspace"
	"path/filepath"
	"sync"
	"testing"
)

type snapshotRecordingObserver struct {
	mu     sync.Mutex
	events []observe.ExecutionEvent
}

func (o *snapshotRecordingObserver) OnLLMCall(context.Context, observe.LLMCallEvent)      {}
func (o *snapshotRecordingObserver) OnToolCall(context.Context, observe.ToolCallEvent)    {}
func (o *snapshotRecordingObserver) OnApproval(context.Context, io.ApprovalEvent)    {}
func (o *snapshotRecordingObserver) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (o *snapshotRecordingObserver) OnError(context.Context, observe.ErrorEvent)          {}
func (o *snapshotRecordingObserver) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}

func (o *snapshotRecordingObserver) snapshot() []observe.ExecutionEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]observe.ExecutionEvent, len(o.events))
	copy(out, o.events)
	return out
}

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
	applied, err := applier.Apply(context.Background(), workspace.PatchApplyRequest{
		Patch:  patch,
		Source: workspace.PatchSourceLLM,
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewGitWorktreeSnapshotStore(repo)
	obs := &snapshotRecordingObserver{}
	store.SetObserver(obs)
	snapshot, err := store.Create(context.Background(), workspace.WorktreeSnapshotRequest{
		SessionID: "sess-1",
		Note:      "before review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != workspace.WorktreeSnapshotGhostState {
		t.Fatalf("unexpected snapshot mode %q", snapshot.Mode)
	}
	if snapshot.SessionID != "sess-1" {
		t.Fatalf("unexpected snapshot session id %q", snapshot.SessionID)
	}
	if len(snapshot.Patches) != 1 || snapshot.Patches[0].PatchID != applied.PatchID {
		t.Fatalf("expected one patch ref, got %+v", snapshot.Patches)
	}
	events := obs.snapshot()
	if len(events) != 1 || events[0].Type != observe.ExecutionSnapshotCreated {
		t.Fatalf("expected snapshot.created event, got %+v", events)
	}
	if events[0].SessionID != "sess-1" {
		t.Fatalf("unexpected event session id %+v", events[0])
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
	bySession, err := store.FindBySession(context.Background(), "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession) != 1 || bySession[0].ID != snapshot.ID {
		t.Fatalf("unexpected session snapshot list %+v", bySession)
	}
}

func TestGitWorktreeSnapshotStore_Unavailable(t *testing.T) {
	store := NewGitWorktreeSnapshotStore(t.TempDir())
	_, err := store.Create(context.Background(), workspace.WorktreeSnapshotRequest{})
	if err != workspace.ErrWorktreeSnapshotUnavailable {
		t.Fatalf("expected ErrWorktreeSnapshotUnavailable, got %v", err)
	}
}
