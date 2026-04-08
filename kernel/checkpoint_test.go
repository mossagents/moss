package kernel

import (
	"context"
	"errors"
	ckpt "github.com/mossagents/moss/kernel/checkpoint"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	kws "github.com/mossagents/moss/kernel/workspace"
	"testing"
)

type stubCheckpointStore struct {
	createFn        func(context.Context, ckpt.CheckpointCreateRequest) (*ckpt.CheckpointRecord, error)
	loadFn          func(context.Context, string) (*ckpt.CheckpointRecord, error)
	findBySessionFn func(context.Context, string) ([]ckpt.CheckpointRecord, error)
}

func (s *stubCheckpointStore) Create(ctx context.Context, req ckpt.CheckpointCreateRequest) (*ckpt.CheckpointRecord, error) {
	if s.createFn != nil {
		return s.createFn(ctx, req)
	}
	return nil, nil
}

func (s *stubCheckpointStore) Load(ctx context.Context, id string) (*ckpt.CheckpointRecord, error) {
	if s.loadFn != nil {
		return s.loadFn(ctx, id)
	}
	return nil, nil
}

func (*stubCheckpointStore) List(context.Context) ([]ckpt.CheckpointRecord, error) { return nil, nil }

func (s *stubCheckpointStore) FindBySession(ctx context.Context, sessionID string) ([]ckpt.CheckpointRecord, error) {
	if s.findBySessionFn != nil {
		return s.findBySessionFn(ctx, sessionID)
	}
	return nil, nil
}

type stubSnapshotStore struct {
	createFn func(context.Context, kws.WorktreeSnapshotRequest) (*kws.WorktreeSnapshot, error)
	loadFn   func(context.Context, string) (*kws.WorktreeSnapshot, error)
}

func (s *stubSnapshotStore) Create(ctx context.Context, req kws.WorktreeSnapshotRequest) (*kws.WorktreeSnapshot, error) {
	if s.createFn != nil {
		return s.createFn(ctx, req)
	}
	return nil, nil
}

func (s *stubSnapshotStore) Load(ctx context.Context, id string) (*kws.WorktreeSnapshot, error) {
	if s.loadFn != nil {
		return s.loadFn(ctx, id)
	}
	return nil, nil
}

func (*stubSnapshotStore) List(context.Context) ([]kws.WorktreeSnapshot, error) { return nil, nil }
func (*stubSnapshotStore) FindBySession(context.Context, string) ([]kws.WorktreeSnapshot, error) {
	return nil, nil
}

type stubPatchRevert struct {
	revertFn func(context.Context, kws.PatchRevertRequest) (*kws.PatchRevertResult, error)
}

func (s *stubPatchRevert) Revert(ctx context.Context, req kws.PatchRevertRequest) (*kws.PatchRevertResult, error) {
	if s.revertFn != nil {
		return s.revertFn(ctx, req)
	}
	return nil, nil
}

func TestKernelCreateCheckpointPersistsFrozenSessionAndSnapshot(t *testing.T) {
	store := mustFileStore(t)
	createdReqs := make([]ckpt.CheckpointCreateRequest, 0, 1)
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			createFn: func(_ context.Context, req ckpt.CheckpointCreateRequest) (*ckpt.CheckpointRecord, error) {
				createdReqs = append(createdReqs, req)
				return &ckpt.CheckpointRecord{
					ID:                 "checkpoint-1",
					SessionID:          req.SessionID,
					WorktreeSnapshotID: req.WorktreeSnapshotID,
					PatchIDs:           append([]string(nil), req.PatchIDs...),
					Lineage:            append([]ckpt.CheckpointLineageRef(nil), req.Lineage...),
				}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			createFn: func(_ context.Context, req kws.WorktreeSnapshotRequest) (*kws.WorktreeSnapshot, error) {
				return &kws.WorktreeSnapshot{
					ID:        "snapshot-1",
					SessionID: req.SessionID,
					Patches:   []kws.PatchSnapshotRef{{PatchID: "patch-1"}},
				}, nil
			},
		}),
	)
	source := &session.Session{
		ID:     "sess-source",
		Status: session.StatusRunning,
		Config: session.SessionConfig{Goal: "ship it"},
		Messages: []mdl.Message{
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hello")}},
		},
	}

	record, err := k.CreateCheckpoint(context.Background(), source, ckpt.CheckpointCreateRequest{
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
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("user")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("assistant")}},
		},
	}); err != nil {
		t.Fatalf("save checkpoint session: %v", err)
	}
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			findBySessionFn: func(_ context.Context, sessionID string) ([]ckpt.CheckpointRecord, error) {
				if sessionID != "sess-live" {
					t.Fatalf("unexpected sessionID %q", sessionID)
				}
				return []ckpt.CheckpointRecord{{
					ID:                 "checkpoint-1",
					SessionID:          "checkpoint-session-1",
					WorktreeSnapshotID: "snapshot-1",
				}}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			loadFn: func(_ context.Context, id string) (*kws.WorktreeSnapshot, error) {
				return &kws.WorktreeSnapshot{
					ID: id,
					Capture: kws.RepoState{
						IsDirty:   true,
						Staged:    []kws.RepoFileState{{Path: "a.go", Status: "M"}},
						Untracked: []string{"tmp.txt"},
					},
					Patches: []kws.PatchSnapshotRef{{PatchID: "patch-1"}},
				}, nil
			},
		}),
		WithPatchRevert(&stubPatchRevert{
			revertFn: func(_ context.Context, req kws.PatchRevertRequest) (*kws.PatchRevertResult, error) {
				if req.Capture == nil || !req.RestoreTracked || !req.RestoreUntracked {
					t.Fatalf("unexpected revert request %+v", req)
				}
				return &kws.PatchRevertResult{Reverted: true}, nil
			},
		}),
	)

	cloned, result, err := k.ForkSession(context.Background(), ckpt.ForkRequest{
		SourceKind:      ckpt.ForkSourceSession,
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
	if result.SourceKind != ckpt.ForkSourceCheckpoint || result.CheckpointID != "checkpoint-1" {
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
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("user")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("assistant")}},
			{Role: mdl.RoleTool, ContentParts: []mdl.ContentPart{mdl.TextPart("tool")}},
		},
		State:  map[string]any{"phase": "mid"},
		Budget: session.Budget{MaxSteps: 10, UsedSteps: 4, UsedTokens: 100},
	}); err != nil {
		t.Fatalf("save checkpoint session: %v", err)
	}
	k := New(
		WithSessionStore(store),
		WithCheckpoints(&stubCheckpointStore{
			loadFn: func(_ context.Context, id string) (*ckpt.CheckpointRecord, error) {
				return &ckpt.CheckpointRecord{ID: id, SessionID: "checkpoint-session-1"}, nil
			},
		}),
	)

	cloned, result, err := k.ReplayFromCheckpoint(context.Background(), ckpt.ReplayRequest{
		CheckpointID: "checkpoint-1",
		Mode:         ckpt.ReplayModeRerun,
	})
	if err != nil {
		t.Fatalf("ReplayFromCheckpoint: %v", err)
	}
	if result.Mode != ckpt.ReplayModeRerun {
		t.Fatalf("mode = %q", result.Mode)
	}
	if len(cloned.Messages) != 2 {
		t.Fatalf("messages = %+v", cloned.Messages)
	}
	if cloned.Messages[0].Role != mdl.RoleSystem || cloned.Messages[1].Role != mdl.RoleUser {
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
			loadFn: func(_ context.Context, id string) (*ckpt.CheckpointRecord, error) {
				return &ckpt.CheckpointRecord{ID: id, SessionID: "checkpoint-session-1", WorktreeSnapshotID: "snapshot-1"}, nil
			},
		}),
		WithWorktreeSnapshots(&stubSnapshotStore{
			loadFn: func(_ context.Context, id string) (*kws.WorktreeSnapshot, error) {
				return &kws.WorktreeSnapshot{ID: id}, nil
			},
		}),
		WithPatchRevert(&stubPatchRevert{
			revertFn: func(context.Context, kws.PatchRevertRequest) (*kws.PatchRevertResult, error) {
				return nil, kws.ErrPatchRevertUnavailable
			},
		}),
	)

	_, result, err := k.ReplayFromCheckpoint(context.Background(), ckpt.ReplayRequest{
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
	_, err := k.CreateCheckpoint(context.Background(), &session.Session{ID: "sess-1"}, ckpt.CheckpointCreateRequest{})
	if !errors.Is(err, ckpt.ErrCheckpointNotRecoverable) {
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
