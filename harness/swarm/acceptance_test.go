package swarm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestSwarmAcceptance_RecoverySnapshotPersistsResearchChain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, tasks, artifacts, rt, orchestrator := newAcceptanceRuntime(t, root)

	runID := "swarm-acceptance"
	seed, err := orchestrator.Seed(ResearchRunSeed{
		RunID:         runID,
		Goal:          "Map the project constraints and deliver a research synthesis",
		RootSessionID: runID + "-supervisor",
		WorkspaceID:   "workspace-1",
	})
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	persistSeedSessions(t, ctx, store, seed)
	for _, item := range seed.Tasks {
		item.SessionID = item.ThreadID
		if err := tasks.UpsertTask(ctx, item); err != nil {
			t.Fatalf("UpsertTask seed %s: %v", item.ID, err)
		}
	}

	workerSessionID := runID + "-worker-1"
	saveThreadSession(t, ctx, store, workerSessionID, seed.Run.RootSessionID, "", runID, kswarm.RoleWorker, time.Now().UTC())
	workerTask := orchestrator.WorkerTask(runID, workerSessionID, "Collect evidence from the repo", "focus on runtime and recovery", []string{seed.Tasks[0].ID})
	workerTask.SessionID = workerSessionID
	workerTask.Status = taskrt.TaskCompleted
	workerTask.Result = "findings collected"
	findingID := saveArtifactRef(t, ctx, artifacts, workerSessionID, kswarm.ArtifactRef{
		RunID:    runID,
		ThreadID: workerSessionID,
		TaskID:   workerTask.ID,
		Name:     "finding-1",
		Kind:     kswarm.ArtifactFinding,
		Summary:  "runtime facts",
	})
	sourceSetID := saveArtifactRef(t, ctx, artifacts, workerSessionID, kswarm.ArtifactRef{
		RunID:    runID,
		ThreadID: workerSessionID,
		TaskID:   workerTask.ID,
		Name:     "sources-1",
		Kind:     kswarm.ArtifactSourceSet,
		Summary:  "source corpus",
	})
	workerTask.ArtifactIDs = []string{findingID, sourceSetID}
	if err := tasks.UpsertTask(ctx, workerTask); err != nil {
		t.Fatalf("UpsertTask worker: %v", err)
	}

	synthThreadID := runID + "-synthesizer"
	synthTask := orchestrator.SynthesisTask(runID, synthThreadID, "Draft the final synthesis", "use collected findings", []string{workerTask.ID})
	synthTask.SessionID = synthThreadID
	synthTask.Status = taskrt.TaskCompleted
	synthTask.Result = "draft produced"
	draftID := saveArtifactRef(t, ctx, artifacts, synthThreadID, kswarm.ArtifactRef{
		RunID:    runID,
		ThreadID: synthThreadID,
		TaskID:   synthTask.ID,
		Name:     "draft-1",
		Kind:     kswarm.ArtifactSynthesisDraft,
		Summary:  "synthesis draft",
	})
	synthTask.ArtifactIDs = []string{draftID}
	if err := tasks.UpsertTask(ctx, synthTask); err != nil {
		t.Fatalf("UpsertTask synthesis: %v", err)
	}

	reviewThreadID := runID + "-reviewer"
	reviewTask := orchestrator.ReviewTask(runID, reviewThreadID, "Review the synthesis draft", "validate evidence coverage", []string{synthTask.ID})
	reviewTask.SessionID = reviewThreadID
	reviewTask.Status = taskrt.TaskCompleted
	reviewTask.Result = "approved"
	if err := tasks.UpsertTask(ctx, reviewTask); err != nil {
		t.Fatalf("UpsertTask review: %v", err)
	}

	if _, err := tasks.EnqueueTaskMessage(ctx, orchestrator.ReviewRequestMessage(runID, synthThreadID, reviewThreadID, reviewTask.ID, "review request", "please review the draft", "quality gate")); err != nil {
		t.Fatalf("EnqueueTaskMessage review: %v", err)
	}

	assertSnapshotResearchChain(t, snapshotOrFatal(t, ctx, rt, runID))

	_, _, _, reopenedRuntime, _ := newAcceptanceRuntime(t, root)
	assertSnapshotResearchChain(t, snapshotOrFatal(t, ctx, reopenedRuntime, runID))
}

func TestSwarmAcceptance_GovernanceInterventionsPersistAcrossRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, tasks, _, rt, orchestrator := newAcceptanceRuntime(t, root)

	runID := "swarm-governance"
	seed, err := orchestrator.Seed(ResearchRunSeed{
		RunID:         runID,
		Goal:          "Investigate failure recovery",
		RootSessionID: runID + "-supervisor",
	})
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	persistSeedSessions(t, ctx, store, seed)
	for _, item := range seed.Tasks {
		item.SessionID = item.ThreadID
		if err := tasks.UpsertTask(ctx, item); err != nil {
			t.Fatalf("UpsertTask seed %s: %v", item.ID, err)
		}
	}

	workerSessionID := runID + "-worker-1"
	saveThreadSession(t, ctx, store, workerSessionID, seed.Run.RootSessionID, "", runID, kswarm.RoleWorker, time.Now().UTC())
	workerTask := orchestrator.WorkerTask(runID, workerSessionID, "Retry collection after a failure", "focus on the broken path", []string{seed.Tasks[0].ID})
	workerTask.SessionID = workerSessionID
	workerTask.Status = taskrt.TaskFailed
	workerTask.Error = "insufficient evidence"
	if err := tasks.UpsertTask(ctx, workerTask); err != nil {
		t.Fatalf("UpsertTask worker: %v", err)
	}

	for _, message := range []taskrt.TaskMessage{
		orchestrator.ReviewRequestMessage(runID, seed.Run.RootSessionID, runID+"-reviewer", workerTask.ID, "review this failure", "need a second opinion", "manual checkpoint"),
		orchestrator.RedirectMessage(runID, seed.Run.RootSessionID, workerSessionID, workerTask.ID, "redirect", "collect broader evidence", "coverage too narrow"),
		orchestrator.TakeoverMessage(runID, runID+"-reviewer", seed.Run.RootSessionID, workerTask.ID, "takeover", "supervisor should finish this", "worker is blocked"),
	} {
		if _, err := tasks.EnqueueTaskMessage(ctx, message); err != nil {
			t.Fatalf("EnqueueTaskMessage(%s): %v", message.Subject, err)
		}
	}

	assertGovernanceSnapshot(t, snapshotOrFatal(t, ctx, rt, runID))

	_, _, _, reopenedRuntime, _ := newAcceptanceRuntime(t, root)
	assertGovernanceSnapshot(t, snapshotOrFatal(t, ctx, reopenedRuntime, runID))
}

func newAcceptanceRuntime(t *testing.T, root string) (*session.FileStore, *taskrt.FileTaskRuntime, *artifact.FileStore, *Runtime, *ResearchOrchestrator) {
	t.Helper()
	store, err := session.NewFileStore(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	tasks, err := taskrt.NewFileTaskRuntime(filepath.Join(root, "tasks"))
	if err != nil {
		t.Fatalf("NewFileTaskRuntime: %v", err)
	}
	artifacts, err := artifact.NewFileStore(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("NewFileStore artifacts: %v", err)
	}
	rt, err := NewRuntime(kernel.New(
		kernel.WithSessionStore(store),
		kernel.WithTaskRuntime(tasks),
		kernel.WithArtifactStore(artifacts),
	))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	orchestrator, err := rt.ResearchOrchestrator()
	if err != nil {
		t.Fatalf("ResearchOrchestrator: %v", err)
	}
	return store, tasks, artifacts, rt, orchestrator
}

func persistSeedSessions(t *testing.T, ctx context.Context, store *session.FileStore, seed *ResearchSeed) {
	t.Helper()
	now := time.Now().UTC()
	for idx, thread := range seed.Threads {
		saveThreadSession(t, ctx, store, thread.ID, thread.ParentThreadID, thread.TaskID, thread.RunID, thread.Role, now.Add(time.Duration(idx)*time.Millisecond))
	}
}

func saveThreadSession(t *testing.T, ctx context.Context, store *session.FileStore, id, parentID, taskID, runID string, role kswarm.Role, createdAt time.Time) {
	t.Helper()
	sess := &session.Session{
		ID:        id,
		Status:    session.StatusRunning,
		CreatedAt: createdAt,
		Config: session.SessionConfig{
			Goal: "swarm acceptance thread",
		},
	}
	if parentID != "" {
		session.SetThreadParent(sess, parentID)
	}
	if taskID != "" {
		session.SetThreadTaskID(sess, taskID)
	}
	session.SetThreadSwarmRunID(sess, runID)
	session.SetThreadRole(sess, string(role))
	session.SetThreadPreview(sess, string(role)+" thread")
	session.RefreshThreadMetadata(sess, createdAt, "acceptance")
	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save session %s: %v", id, err)
	}
}

func saveArtifactRef(t *testing.T, ctx context.Context, store *artifact.FileStore, sessionID string, ref kswarm.ArtifactRef) string {
	t.Helper()
	item := &artifact.Artifact{
		Name:     ref.Name,
		MIMEType: "text/plain",
		Data:     []byte(ref.Summary),
	}
	kswarm.StampArtifact(item, ref)
	if err := store.Save(ctx, sessionID, item); err != nil {
		t.Fatalf("Save artifact %s: %v", ref.Name, err)
	}
	return item.ID
}

func snapshotOrFatal(t *testing.T, ctx context.Context, rt *Runtime, runID string) *kswarm.Snapshot {
	t.Helper()
	snapshot, err := rt.Snapshot(ctx, runID, false)
	if err != nil {
		t.Fatalf("Snapshot(%s): %v", runID, err)
	}
	return snapshot
}

func assertSnapshotResearchChain(t *testing.T, snapshot *kswarm.Snapshot) {
	t.Helper()
	if snapshot.RootSessionID != "swarm-acceptance-supervisor" {
		t.Fatalf("unexpected root session: %+v", snapshot)
	}
	if len(snapshot.Threads) != 5 || len(snapshot.Tasks) != 5 || len(snapshot.Messages) != 1 || len(snapshot.Artifacts) != 3 {
		t.Fatalf("unexpected snapshot sizes: threads=%d tasks=%d messages=%d artifacts=%d", len(snapshot.Threads), len(snapshot.Tasks), len(snapshot.Messages), len(snapshot.Artifacts))
	}
	kindCounts := make(map[kswarm.ArtifactKind]int)
	for _, item := range snapshot.Artifacts {
		kindCounts[item.Kind]++
	}
	for _, kind := range []kswarm.ArtifactKind{kswarm.ArtifactFinding, kswarm.ArtifactSourceSet, kswarm.ArtifactSynthesisDraft} {
		if kindCounts[kind] != 1 {
			t.Fatalf("expected artifact kind %s once, got %d", kind, kindCounts[kind])
		}
	}
}

func assertGovernanceSnapshot(t *testing.T, snapshot *kswarm.Snapshot) {
	t.Helper()
	if snapshot.RootSessionID != "swarm-governance-supervisor" {
		t.Fatalf("unexpected root session: %+v", snapshot)
	}
	if len(snapshot.Messages) != 3 {
		t.Fatalf("expected 3 governance messages, got %d", len(snapshot.Messages))
	}
	actions := make(map[kswarm.GovernanceAction]int)
	for _, message := range snapshot.Messages {
		actions[kswarm.GovernanceActionFromMetadata(message.Metadata)]++
	}
	if actions[kswarm.GovernanceReviewRequested] != 1 || actions[kswarm.GovernanceRedirected] != 1 || actions[kswarm.GovernanceTakenOver] != 1 {
		t.Fatalf("unexpected governance actions: %+v", actions)
	}
	failed := 0
	for _, task := range snapshot.Tasks {
		if task.Status == taskrt.TaskFailed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected one failed task, got %d", failed)
	}
}
