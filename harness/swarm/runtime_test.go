package swarm

import (
	"context"
	"testing"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestNewRuntimeAndSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	tasks := taskrt.NewMemoryTaskRuntime()
	artifacts := artifact.NewMemoryStore()
	k := kernel.New(
		kernel.WithSessionStore(store),
		kernel.WithTaskRuntime(tasks),
		kernel.WithArtifactStore(artifacts),
	)

	root := &session.Session{
		ID:        "sess-root",
		Status:    session.StatusRunning,
		Config:    session.SessionConfig{Goal: "research"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	session.SetThreadSwarmRunID(root, "swarm-1")
	session.SetThreadRole(root, "supervisor")
	session.RefreshThreadMetadata(root, root.CreatedAt, "assistant")
	if err := store.Save(ctx, root); err != nil {
		t.Fatalf("Save root: %v", err)
	}
	if err := tasks.UpsertTask(ctx, taskrt.TaskRecord{
		ID:         "task-1",
		Goal:       "collect",
		Status:     taskrt.TaskPending,
		SwarmRunID: "swarm-1",
		ThreadID:   "sess-root",
		SessionID:  "sess-root",
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	rt, err := NewRuntime(k)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	snapshot, err := rt.Snapshot(ctx, "swarm-1", false)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.RunID != "swarm-1" || snapshot.RootSessionID != "sess-root" || len(snapshot.Tasks) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if _, ok := rt.Role(kswarm.RoleSupervisor); !ok {
		t.Fatal("expected supervisor role in runtime role pack")
	}
}

func TestRuntimeFeatureAttachesServiceAndRoles(t *testing.T) {
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	k := kernel.New(
		kernel.WithSessionStore(store),
		kernel.WithTaskRuntime(taskrt.NewMemoryTaskRuntime()),
		kernel.WithArtifactStore(artifact.NewMemoryStore()),
	)
	h := harness.New(k, nil)
	if err := h.Install(context.Background(), RuntimeFeature()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	rt := RuntimeOf(k)
	if rt == nil {
		t.Fatal("expected runtime service to be attached")
	}
	if _, ok := harness.SubagentCatalogOf(k).Get("swarm-supervisor"); !ok {
		t.Fatal("expected swarm-supervisor to be registered")
	}
	orchestrator, err := rt.ResearchOrchestrator()
	if err != nil {
		t.Fatalf("ResearchOrchestrator: %v", err)
	}
	if orchestrator == nil {
		t.Fatal("expected research orchestrator")
	}
}
