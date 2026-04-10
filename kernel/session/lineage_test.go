package session

import (
	"context"
	"github.com/mossagents/moss/kernel/checkpoint"
	"path/filepath"
	"testing"
	"time"
)

type checkpointCatalogStub struct {
	records map[string]checkpoint.CheckpointRecord
}

func (s checkpointCatalogStub) Create(context.Context, checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
	return nil, checkpoint.ErrCheckpointUnavailable
}

func (s checkpointCatalogStub) Load(_ context.Context, id string) (*checkpoint.CheckpointRecord, error) {
	record, ok := s.records[id]
	if !ok {
		return nil, nil
	}
	cp := record
	return &cp, nil
}

func (s checkpointCatalogStub) List(context.Context) ([]checkpoint.CheckpointRecord, error) {
	out := make([]checkpoint.CheckpointRecord, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, record)
	}
	return out, nil
}

func (s checkpointCatalogStub) FindBySession(_ context.Context, sessionID string) ([]checkpoint.CheckpointRecord, error) {
	var out []checkpoint.CheckpointRecord
	for _, record := range s.records {
		if record.SessionID == sessionID {
			out = append(out, record)
		}
	}
	return out, nil
}

func TestCatalogListsThreadsAndCheckpoints(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID:        "sess-1",
		Status:    StatusRunning,
		Config:    SessionConfig{Goal: "ship codex-style resume", Mode: "interactive", Profile: "default"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	SetThreadParent(sess, "sess-root")
	SetThreadTaskID(sess, "task-1")
	SetThreadPreview(sess, "resume picker")
	TouchThreadActivity(sess, time.Now().UTC(), "assistant")
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	catalog := Catalog{
		Store: store,
		Checkpoints: checkpointCatalogStub{
			records: map[string]checkpoint.CheckpointRecord{
				"cp-1": {
					ID:        "cp-1",
					SessionID: "sess-1",
					Note:      "before fork",
					Lineage: []checkpoint.CheckpointLineageRef{
						{Kind: checkpoint.CheckpointLineageSession, ID: "sess-1"},
					},
					CreatedAt: time.Now().UTC(),
				},
			},
		},
	}

	threads, err := catalog.ListThreads(context.Background(), ThreadQuery{RecoverableOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ParentSessionID != "sess-root" || threads[0].TaskID != "task-1" {
		t.Fatalf("unexpected threads: %+v", threads)
	}
	checkpoints, err := catalog.ListCheckpoints(context.Background(), CheckpointQuery{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 || checkpoints[0].ID != "cp-1" {
		t.Fatalf("unexpected checkpoints: %+v", checkpoints)
	}
	source, err := catalog.ResolveForkSource(context.Background(), checkpoint.ForkSourceCheckpoint, "cp-1")
	if err != nil {
		t.Fatal(err)
	}
	if source == nil || source.CheckpointID != "cp-1" || source.SessionID != "sess-1" {
		t.Fatalf("unexpected fork source: %+v", source)
	}
}
