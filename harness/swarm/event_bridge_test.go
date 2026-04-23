package swarm

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestInstallEventBridges_EmitExecutionAndRuntimeSwarmEvents(t *testing.T) {
	ctx := context.Background()
	sessionStore, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	taskRuntime, err := taskrt.NewFileTaskRuntime(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileTaskRuntime: %v", err)
	}
	artifactStore, err := artifact.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore artifact: %v", err)
	}
	eventStore, err := kruntime.NewJSONLEventStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLEventStore: %v", err)
	}
	k := kernel.New(
		kernel.WithSessionStore(sessionStore),
		kernel.WithTaskRuntime(taskRuntime),
		kernel.WithArtifactStore(artifactStore),
		kernel.WithEventStore(eventStore),
	)
	recorder := &recordingSwarmObserver{}
	k.SetObserver(recorder)
	if err := InstallEventBridges(k); err != nil {
		t.Fatalf("InstallEventBridges: %v", err)
	}

	rootSessionID := "sess-root"
	runID := "swarm-events"
	if err := eventStore.AppendEvents(ctx, rootSessionID, 0, "req-session-created", []kruntime.RuntimeEvent{{
		Type:      kruntime.EventTypeSessionCreated,
		Timestamp: time.Now().UTC(),
		Payload: &kruntime.SessionCreatedPayload{
			BlueprintPayload: &kruntime.SessionBlueprint{Identity: kruntime.BlueprintIdentity{SessionID: rootSessionID}},
		},
	}}); err != nil {
		t.Fatalf("AppendEvents session_created: %v", err)
	}

	root := &session.Session{
		ID:        rootSessionID,
		Status:    session.StatusRunning,
		CreatedAt: time.Now().UTC(),
		Config:    session.SessionConfig{Goal: "swarm root"},
	}
	session.SetThreadSwarmRunID(root, runID)
	session.SetThreadRole(root, "supervisor")
	session.SetThreadPreview(root, "supervisor thread")
	session.RefreshThreadMetadata(root, root.CreatedAt, "seed")
	if err := k.SessionStore().Save(ctx, root); err != nil {
		t.Fatalf("Save root: %v", err)
	}

	child := &session.Session{
		ID:        "sess-worker",
		Status:    session.StatusRunning,
		CreatedAt: root.CreatedAt.Add(time.Millisecond),
		Config:    session.SessionConfig{Goal: "worker thread"},
	}
	session.SetThreadParent(child, rootSessionID)
	session.SetThreadSwarmRunID(child, runID)
	session.SetThreadRole(child, "worker")
	session.SetThreadTaskID(child, "task-1")
	session.SetThreadPreview(child, "worker thread")
	session.RefreshThreadMetadata(child, child.CreatedAt, "delegated")
	if err := k.SessionStore().Save(ctx, child); err != nil {
		t.Fatalf("Save child: %v", err)
	}

	if err := k.TaskRuntime().UpsertTask(ctx, taskrt.TaskRecord{
		ID:         "task-1",
		AgentName:  "swarm-worker",
		Goal:       "collect evidence",
		Status:     taskrt.TaskPending,
		SwarmRunID: runID,
		ThreadID:   child.ID,
		SessionID:  child.ID,
	}); err != nil {
		t.Fatalf("UpsertTask create: %v", err)
	}
	if err := k.TaskRuntime().UpsertTask(ctx, taskrt.TaskRecord{
		ID:         "task-1",
		AgentName:  "swarm-worker",
		Goal:       "collect evidence",
		Status:     taskrt.TaskRunning,
		ClaimedBy:  child.ID,
		SwarmRunID: runID,
		ThreadID:   child.ID,
		SessionID:  child.ID,
	}); err != nil {
		t.Fatalf("UpsertTask claim: %v", err)
	}
	msgRuntime, ok := k.TaskRuntime().(taskrt.TaskMessageRuntime)
	if !ok {
		t.Fatal("wrapped task runtime missing TaskMessageRuntime")
	}
	if _, err := msgRuntime.EnqueueTaskMessage(ctx, taskrt.TaskMessage{
		TaskID:       "task-1",
		SwarmRunID:   runID,
		ThreadID:     child.ID,
		FromThreadID: rootSessionID,
		ToThreadID:   child.ID,
		Kind:         string(kswarm.MessageHandoff),
		Subject:      "redirect",
		Content:      "expand evidence",
		Metadata:     kswarm.GovernanceMetadata(kswarm.GovernanceRedirected, "need broader coverage", nil),
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("EnqueueTaskMessage: %v", err)
	}

	item := &artifact.Artifact{Name: "finding-1", MIMEType: "text/plain", Data: []byte("finding")}
	kswarm.StampArtifact(item, kswarm.ArtifactRef{
		RunID:    runID,
		ThreadID: child.ID,
		TaskID:   "task-1",
		Name:     "finding-1",
		Kind:     kswarm.ArtifactFinding,
		Summary:  "worker finding",
	})
	if err := k.ArtifactStore().Save(ctx, child.ID, item); err != nil {
		t.Fatalf("Artifact Save: %v", err)
	}

	root.Status = session.StatusCompleted
	root.EndedAt = time.Now().UTC()
	if err := k.SessionStore().Save(ctx, root); err != nil {
		t.Fatalf("Save root completed: %v", err)
	}

	wantExec := []observe.ExecutionEventType{
		observe.ExecutionSwarmStarted,
		observe.ExecutionSwarmThreadSpawned,
		observe.ExecutionSwarmTaskCreated,
		observe.ExecutionSwarmTaskClaimed,
		observe.ExecutionSwarmMessageSent,
		observe.ExecutionSwarmArtifactPub,
		observe.ExecutionSwarmThreadDone,
		observe.ExecutionSwarmCompleted,
	}
	for _, typ := range wantExec {
		if !slices.Contains(recorder.types, typ) {
			t.Fatalf("missing execution event %s in %+v", typ, recorder.types)
		}
	}
	for _, sessionID := range recorder.sessionIDs {
		if sessionID != rootSessionID {
			t.Fatalf("expected root session id %q for swarm events, got %q", rootSessionID, sessionID)
		}
	}

	events, err := eventStore.LoadEvents(ctx, rootSessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var runtimeTypes []kruntime.EventType
	for _, event := range events {
		runtimeTypes = append(runtimeTypes, event.Type)
	}
	for _, typ := range []kruntime.EventType{
		kruntime.EventTypeSwarmStarted,
		kruntime.EventTypeSwarmThreadSpawned,
		kruntime.EventTypeSwarmTaskCreated,
		kruntime.EventTypeSwarmTaskClaimed,
		kruntime.EventTypeSwarmMessageSent,
		kruntime.EventTypeSwarmArtifactPublished,
		kruntime.EventTypeSwarmThreadCompleted,
		kruntime.EventTypeSwarmCompleted,
	} {
		if !slices.Contains(runtimeTypes, typ) {
			t.Fatalf("missing runtime event %s in %+v", typ, runtimeTypes)
		}
	}
}

type recordingSwarmObserver struct {
	observe.NoOpObserver
	types      []observe.ExecutionEventType
	sessionIDs []string
}

func (o *recordingSwarmObserver) OnExecutionEvent(_ context.Context, event observe.ExecutionEvent) {
	o.types = append(o.types, event.Type)
	o.sessionIDs = append(o.sessionIDs, event.SessionID)
}
