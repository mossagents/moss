package kernel

import (
	"context"
	"errors"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/workspace"
	"testing"
)

type stubCheckpointStore struct {
	createFn        func(context.Context, checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error)
	loadFn          func(context.Context, string) (*checkpoint.CheckpointRecord, error)
	findBySessionFn func(context.Context, string) ([]checkpoint.CheckpointRecord, error)
}

func (s *stubCheckpointStore) Create(ctx context.Context, req checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
	if s.createFn != nil {
		return s.createFn(ctx, req)
	}
	return nil, nil
}

func (s *stubCheckpointStore) Load(ctx context.Context, id string) (*checkpoint.CheckpointRecord, error) {
	if s.loadFn != nil {
		return s.loadFn(ctx, id)
	}
	return nil, nil
}

func (*stubCheckpointStore) List(context.Context) ([]checkpoint.CheckpointRecord, error) { return nil, nil }

func (s *stubCheckpointStore) FindBySession(ctx context.Context, sessionID string) ([]checkpoint.CheckpointRecord, error) {
	if s.findBySessionFn != nil {
		return s.findBySessionFn(ctx, sessionID)
	}
	return nil, nil
}

type stubSnapshotStore struct {
	createFn func(context.Context, workspace.WorktreeSnapshotRequest) (*workspace.WorktreeSnapshot, error)
	loadFn   func(context.Context, string) (*workspace.WorktreeSnapshot, error)
}

func (s *stubSnapshotStore) Create(ctx context.Context, req workspace.WorktreeSnapshotRequest) (*workspace.WorktreeSnapshot, error) {
	if s.createFn != nil {
		return s.createFn(ctx, req)
	}
	return nil, nil
}

func (s *stubSnapshotStore) Load(ctx context.Context, id string) (*workspace.WorktreeSnapshot, error) {
	if s.loadFn != nil {
		return s.loadFn(ctx, id)
	}
	return nil, nil
}

func (*stubSnapshotStore) List(context.Context) ([]workspace.WorktreeSnapshot, error) { return nil, nil }
func (*stubSnapshotStore) FindBySession(context.Context, string) ([]workspace.WorktreeSnapshot, error) {
	return nil, nil
}

type stubPatchRevert struct {
	revertFn func(context.Context, workspace.PatchRevertRequest) (*workspace.PatchRevertResult, error)
}

func (s *stubPatchRevert) Revert(ctx context.Context, req workspace.PatchRevertRequest) (*workspace.PatchRevertResult, error) {
	if s.revertFn != nil {
		return s.revertFn(ctx, req)
	}
	return nil, nil
}

func TestKernelCreateCheckpointPersistsFrozenSessionAndSnapshot(t *testing.T) {
	store := mustFileStore(t)
	createdReqs := make([]checkpoint.CheckpointCreateRequest, 0, 1)
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			createFn: func(_ context.Context, req checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
				createdReqs = append(createdReqs, req)
				return &checkpoint.CheckpointRecord{
					ID:                 "checkpoint-1",
					SessionID:          req.SessionID,
					WorktreeSnapshotID: req.WorktreeSnapshotID,
					PatchIDs:           append([]string(nil), req.PatchIDs...),
					Lineage:            append([]checkpoint.CheckpointLineageRef(nil), req.Lineage...),
				}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			createFn: func(_ context.Context, req workspace.WorktreeSnapshotRequest) (*workspace.WorktreeSnapshot, error) {
				return &workspace.WorktreeSnapshot{
					ID:        "snapshot-1",
					SessionID: req.SessionID,
					Patches:   []workspace.PatchSnapshotRef{{PatchID: "patch-1"}},
				}, nil
			},
		}),
	)
	source := &session.Session{
		ID:     "sess-source",
		Status: session.StatusRunning,
		Config: session.SessionConfig{Goal: "ship it"},
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hello")}},
		},
	}

	record, err := k.CreateCheckpoint(context.Background(), source, checkpoint.CheckpointCreateRequest{
		Note: "before replay",
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if record.ID != "checkpoint-1" {
		t.Fatalf("checkpoint id = %q", record.ID)
	}
	if len(createdReqs) != 1 {
		t.Fatalf("checkpoint create calls = %d", len(createdReqs))
	}
	if createdReqs[0].WorktreeSnapshotID != "snapshot-1" {
		t.Fatalf("snapshot id = %q", createdReqs[0].WorktreeSnapshotID)
	}
	if len(createdReqs[0].PatchIDs) != 1 || createdReqs[0].PatchIDs[0] != "patch-1" {
		t.Fatalf("patch ids = %+v", createdReqs[0].PatchIDs)
	}
	if len(createdReqs[0].Lineage) != 1 || createdReqs[0].Lineage[0].ID != "sess-source" {
		t.Fatalf("lineage = %+v", createdReqs[0].Lineage)
	}

	frozen, err := store.Load(context.Background(), createdReqs[0].SessionID)
	if err != nil {
		t.Fatalf("load frozen session: %v", err)
	}
	if frozen == nil {
		t.Fatal("expected frozen checkpoint session")
	}
	if hidden, _ := frozen.Config.Metadata[checkpointSnapshotHiddenKey].(bool); !hidden {
		t.Fatalf("expected hidden checkpoint snapshot metadata, got %+v", frozen.Config.Metadata)
	}
}

func TestKernelForkSessionPrefersCheckpointAndMarksDegradedRestore(t *testing.T) {
	store := mustFileStore(t)
	if err := store.Save(context.Background(), &session.Session{
		ID:     "checkpoint-session-1",
		Status: session.StatusPaused,
		Config: session.SessionConfig{Goal: "ship it"},
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys")}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("user")}},
			{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("assistant")}},
		},
	}); err != nil {
		t.Fatalf("save checkpoint session: %v", err)
	}
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			findBySessionFn: func(_ context.Context, sessionID string) ([]checkpoint.CheckpointRecord, error) {
				if sessionID != "sess-live" {
					t.Fatalf("unexpected sessionID %q", sessionID)
				}
				return []checkpoint.CheckpointRecord{{
					ID:                 "checkpoint-1",
					SessionID:          "checkpoint-session-1",
					WorktreeSnapshotID: "snapshot-1",
				}}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			loadFn: func(_ context.Context, id string) (*workspace.WorktreeSnapshot, error) {
				return &workspace.WorktreeSnapshot{
					ID: id,
					Capture: workspace.RepoState{
						IsDirty:   true,
						Staged:    []workspace.RepoFileState{{Path: "a.go", Status: "M"}},
						Untracked: []string{"tmp.txt"},
					},
					Patches: []workspace.PatchSnapshotRef{{PatchID: "patch-1"}},
				}, nil
			},
		}),
		WithPatchRevert(&stubPatchRevert{
			revertFn: func(_ context.Context, req workspace.PatchRevertRequest) (*workspace.PatchRevertResult, error) {
				if req.Capture == nil || !req.RestoreTracked || !req.RestoreUntracked {
					t.Fatalf("unexpected revert request %+v", req)
				}
				return &workspace.PatchRevertResult{Reverted: true}, nil
			},
		}),
	)

	cloned, result, err := k.ForkSession(context.Background(), checkpoint.ForkRequest{
		SourceKind:      checkpoint.ForkSourceSession,
		SourceID:        "sess-live",
		RestoreWorktree: true,
		Note:            "fork it",
	})
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if cloned.ID == "" || cloned.ID == "checkpoint-session-1" {
		t.Fatalf("expected new live session id, got %q", cloned.ID)
	}
	if result.SourceKind != checkpoint.ForkSourceCheckpoint || result.CheckpointID != "checkpoint-1" {
		t.Fatalf("unexpected fork result %+v", result)
	}
	if result.RestoredWorktree {
		t.Fatal("expected degraded worktree restore")
	}
	if !result.Degraded {
		t.Fatal("expected degraded result")
	}
}

func TestKernelReplayFromCheckpointRerunKeepsOnlyUserAndSystemMessages(t *testing.T) {
	store := mustFileStore(t)
	if err := store.Save(context.Background(), &session.Session{
		ID:     "checkpoint-session-1",
		Status: session.StatusPaused,
		Config: session.SessionConfig{Goal: "ship it", MaxSteps: 10},
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys")}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("user")}},
			{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("assistant")}},
			{Role: model.RoleTool, ContentParts: []model.ContentPart{model.TextPart("tool")}},
		},
		State:  map[string]any{"phase": "mid"},
		Budget: session.Budget{MaxSteps: 10, UsedSteps: 4, UsedTokens: 100},
	}); err != nil {
		t.Fatalf("save checkpoint session: %v", err)
	}
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			loadFn: func(_ context.Context, id string) (*checkpoint.CheckpointRecord, error) {
				return &checkpoint.CheckpointRecord{ID: id, SessionID: "checkpoint-session-1"}, nil
			},
		}),
	)

	cloned, result, err := k.ReplayFromCheckpoint(context.Background(), checkpoint.ReplayRequest{
		CheckpointID: "checkpoint-1",
		Mode:         checkpoint.ReplayModeRerun,
	})
	if err != nil {
		t.Fatalf("ReplayFromCheckpoint: %v", err)
	}
	if result.Mode != checkpoint.ReplayModeRerun {
		t.Fatalf("mode = %q", result.Mode)
	}
	if len(cloned.Messages) != 2 {
		t.Fatalf("messages = %+v", cloned.Messages)
	}
	if cloned.Messages[0].Role != model.RoleSystem || cloned.Messages[1].Role != model.RoleUser {
		t.Fatalf("unexpected rerun messages %+v", cloned.Messages)
	}
	if cloned.Budget.UsedSteps != 0 || cloned.Budget.UsedTokens != 0 {
		t.Fatalf("expected budget reset, got steps=%d tokens=%d", cloned.Budget.UsedSteps, cloned.Budget.UsedTokens)
	}
	if len(cloned.State) != 0 {
		t.Fatalf("expected empty state, got %+v", cloned.State)
	}
}

func TestKernelReplayFromCheckpointReportsUnavailableRestoreAsDegraded(t *testing.T) {
	store := mustFileStore(t)
	if err := store.Save(context.Background(), &session.Session{
		ID:     "checkpoint-session-1",
		Status: session.StatusPaused,
		Config: session.SessionConfig{Goal: "ship it"},
	}); err != nil {
		t.Fatalf("save checkpoint session: %v", err)
	}
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			loadFn: func(_ context.Context, id string) (*checkpoint.CheckpointRecord, error) {
				return &checkpoint.CheckpointRecord{ID: id, SessionID: "checkpoint-session-1", WorktreeSnapshotID: "snapshot-1"}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			loadFn: func(_ context.Context, id string) (*workspace.WorktreeSnapshot, error) {
				return &workspace.WorktreeSnapshot{ID: id}, nil
			},
		}),
		WithPatchRevert(&stubPatchRevert{
			revertFn: func(context.Context, workspace.PatchRevertRequest) (*workspace.PatchRevertResult, error) {
				return nil, workspace.ErrPatchRevertUnavailable
			},
		}),
	)

	_, result, err := k.ReplayFromCheckpoint(context.Background(), checkpoint.ReplayRequest{
		CheckpointID:    "checkpoint-1",
		RestoreWorktree: true,
	})
	if err != nil {
		t.Fatalf("ReplayFromCheckpoint: %v", err)
	}
	if result.RestoredWorktree {
		t.Fatal("expected restore to be degraded")
	}
	if !result.Degraded {
		t.Fatal("expected degraded replay result")
	}
	if result.Details == "" {
		t.Fatal("expected degraded details")
	}
}

func TestKernelCreateCheckpointRequiresSessionStore(t *testing.T) {
	k := New(
		WithCheckpoints(&stubCheckpointStore{}),
	)
	_, err := k.CreateCheckpoint(context.Background(), &session.Session{ID: "sess-1"}, checkpoint.CheckpointCreateRequest{})
	if !errors.Is(err, checkpoint.ErrCheckpointNotRecoverable) {
		t.Fatalf("expected ErrCheckpointNotRecoverable, got %v", err)
	}
}

func mustFileStore(t *testing.T) *session.FileStore {
	t.Helper()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store
}
