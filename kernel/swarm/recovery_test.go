package swarm

import (
	"context"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestRecoveryResolverLoadRun(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	root := &session.Session{
		ID:        "sess-root",
		Status:    session.StatusRunning,
		Config:    session.SessionConfig{Goal: "research"},
		CreatedAt: time.Now().Add(-2 * time.Minute),
	}
	session.SetThreadSwarmRunID(root, "swarm-1")
	session.SetThreadRole(root, "supervisor")
	session.SetThreadPreview(root, "root")
	session.RefreshThreadMetadata(root, root.CreatedAt, "assistant")
	if err := store.Save(ctx, root); err != nil {
		t.Fatalf("save root: %v", err)
	}

	worker := &session.Session{
		ID:        "sess-worker",
		Status:    session.StatusRunning,
		Config:    session.SessionConfig{Goal: "search"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	session.SetThreadParent(worker, "sess-root")
	session.SetThreadTaskID(worker, "task-1")
	session.SetThreadSwarmRunID(worker, "swarm-1")
	session.SetThreadRole(worker, "worker")
	session.SetThreadPreview(worker, "worker")
	session.RefreshThreadMetadata(worker, worker.CreatedAt, "tool:task")
	if err := store.Save(ctx, worker); err != nil {
		t.Fatalf("save worker: %v", err)
	}

	tasks := taskrt.NewMemoryTaskRuntime()
	if err := tasks.UpsertTask(ctx, taskrt.TaskRecord{
		ID:          "task-1",
		Goal:        "collect sources",
		Status:      taskrt.TaskRunning,
		SwarmRunID:  "swarm-1",
		ThreadID:    "sess-worker",
		SessionID:   "sess-worker",
		ArtifactIDs: []string{"artifact-1"},
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if _, err := tasks.EnqueueTaskMessage(ctx, taskrt.TaskMessage{
		TaskID:       "task-1",
		SwarmRunID:   "swarm-1",
		ThreadID:     "sess-worker",
		FromThreadID: "sess-worker",
		ToThreadID:   "sess-root",
		Kind:         "status",
		Content:      "searching",
	}); err != nil {
		t.Fatalf("EnqueueTaskMessage: %v", err)
	}

	artifactStore := artifact.NewMemoryStore()
	art := &artifact.Artifact{Name: "sources", MIMEType: "application/json", Data: []byte(`[]`)}
	StampArtifact(art, ArtifactRef{
		RunID:    "swarm-1",
		ThreadID: "sess-worker",
		TaskID:   "task-1",
		Name:     "sources",
		Kind:     ArtifactSourceSet,
		Summary:  "raw sources",
	})
	if err := artifactStore.Save(ctx, "sess-worker", art); err != nil {
		t.Fatalf("Save artifact: %v", err)
	}

	resolver := RecoveryResolver{
		Sessions:  session.Catalog{Store: store},
		Tasks:     tasks,
		Messages:  tasks,
		Artifacts: artifactStore,
	}
	snapshot, err := resolver.LoadRun(ctx, RecoveryQuery{RunID: "swarm-1"})
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if snapshot.RootSessionID != "sess-root" {
		t.Fatalf("RootSessionID = %q", snapshot.RootSessionID)
	}
	if len(snapshot.Threads) != 2 || len(snapshot.Tasks) != 1 || len(snapshot.Messages) != 1 || len(snapshot.Artifacts) != 1 {
		t.Fatalf("unexpected snapshot sizes: %+v", snapshot)
	}
	if snapshot.Messages[0].ToThreadID != "sess-root" || snapshot.Artifacts[0].Kind != ArtifactSourceSet {
		t.Fatalf("unexpected snapshot content: %+v", snapshot)
	}
}
