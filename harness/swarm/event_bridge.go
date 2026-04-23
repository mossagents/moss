package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type swarmEventEmitter struct {
	k *kernel.Kernel
}

func InstallEventBridges(k *kernel.Kernel) error {
	if k == nil {
		return fmt.Errorf("kernel is required")
	}
	emitter := &swarmEventEmitter{k: k}
	if store := k.SessionStore(); store != nil {
		if err := k.Apply(kernel.WithSessionStore(WrapSessionStore(store, emitter))); err != nil {
			return err
		}
	}
	if tasks := k.TaskRuntime(); tasks != nil {
		if err := k.Apply(kernel.WithTaskRuntime(WrapTaskRuntime(tasks, emitter))); err != nil {
			return err
		}
	}
	if artifacts := k.ArtifactStore(); artifacts != nil {
		if err := k.Apply(kernel.WithArtifactStore(WrapArtifactStore(artifacts, emitter))); err != nil {
			return err
		}
	}
	return nil
}

type swarmWrappedMarker interface {
	swarmWrapped()
}

type eventedSessionStore struct {
	inner   session.SessionStore
	emitter *swarmEventEmitter
}

func (s *eventedSessionStore) swarmWrapped() {}

func WrapSessionStore(store session.SessionStore, emitter *swarmEventEmitter) session.SessionStore {
	if store == nil || emitter == nil {
		return store
	}
	if _, ok := store.(swarmWrappedMarker); ok {
		return store
	}
	return &eventedSessionStore{inner: store, emitter: emitter}
}

func (s *eventedSessionStore) Save(ctx context.Context, sess *session.Session) error {
	var before *session.Session
	if sess != nil && strings.TrimSpace(sess.ID) != "" {
		before, _ = s.inner.Load(ctx, sess.ID)
	}
	if err := s.inner.Save(ctx, sess); err != nil {
		return err
	}
	s.emitter.emitSession(ctx, before, sess)
	return nil
}

func (s *eventedSessionStore) Load(ctx context.Context, id string) (*session.Session, error) {
	return s.inner.Load(ctx, id)
}

func (s *eventedSessionStore) List(ctx context.Context) ([]session.SessionSummary, error) {
	return s.inner.List(ctx)
}

func (s *eventedSessionStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s *eventedSessionStore) Watch(ctx context.Context, id string) (<-chan *session.Session, error) {
	watchable, ok := s.inner.(session.WatchableSessionStore)
	if !ok {
		return nil, session.ErrNotSupported
	}
	return watchable.Watch(ctx, id)
}

type eventedTaskRuntime struct {
	inner   taskrt.TaskRuntime
	emitter *swarmEventEmitter
}

func (r *eventedTaskRuntime) swarmWrapped() {}

func WrapTaskRuntime(inner taskrt.TaskRuntime, emitter *swarmEventEmitter) taskrt.TaskRuntime {
	if inner == nil || emitter == nil {
		return inner
	}
	if _, ok := inner.(swarmWrappedMarker); ok {
		return inner
	}
	return &eventedTaskRuntime{inner: inner, emitter: emitter}
}

func (r *eventedTaskRuntime) UpsertTask(ctx context.Context, task taskrt.TaskRecord) error {
	var before *taskrt.TaskRecord
	if strings.TrimSpace(task.ID) != "" {
		before, _ = r.inner.GetTask(ctx, task.ID)
	}
	if err := r.inner.UpsertTask(ctx, task); err != nil {
		return err
	}
	r.emitter.emitTask(ctx, before, task)
	return nil
}

func (r *eventedTaskRuntime) GetTask(ctx context.Context, id string) (*taskrt.TaskRecord, error) {
	return r.inner.GetTask(ctx, id)
}

func (r *eventedTaskRuntime) ListTasks(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskRecord, error) {
	return r.inner.ListTasks(ctx, query)
}

func (r *eventedTaskRuntime) ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*taskrt.TaskRecord, error) {
	return r.inner.ClaimNextReady(ctx, claimer, preferredAgent)
}

func (r *eventedTaskRuntime) ListTaskSummaries(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskSummary, error) {
	graph, ok := r.inner.(taskrt.TaskGraphRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement TaskGraphRuntime")
	}
	return graph.ListTaskSummaries(ctx, query)
}

func (r *eventedTaskRuntime) ListTaskRelations(ctx context.Context, taskID string) ([]taskrt.TaskRelation, error) {
	graph, ok := r.inner.(taskrt.TaskGraphRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement TaskGraphRuntime")
	}
	return graph.ListTaskRelations(ctx, taskID)
}

func (r *eventedTaskRuntime) EnqueueTaskMessage(ctx context.Context, message taskrt.TaskMessage) (*taskrt.TaskMessage, error) {
	queue, ok := r.inner.(taskrt.TaskMessageRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement TaskMessageRuntime")
	}
	stored, err := queue.EnqueueTaskMessage(ctx, message)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		r.emitter.emitMessage(ctx, *stored)
	}
	return stored, nil
}

func (r *eventedTaskRuntime) ListTaskMessages(ctx context.Context, taskID string, limit int) ([]taskrt.TaskMessage, error) {
	queue, ok := r.inner.(taskrt.TaskMessageRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement TaskMessageRuntime")
	}
	return queue.ListTaskMessages(ctx, taskID, limit)
}

func (r *eventedTaskRuntime) ConsumeTaskMessages(ctx context.Context, taskID string, limit int) ([]taskrt.TaskMessage, error) {
	queue, ok := r.inner.(taskrt.TaskMessageRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement TaskMessageRuntime")
	}
	return queue.ConsumeTaskMessages(ctx, taskID, limit)
}

func (r *eventedTaskRuntime) UpsertJob(ctx context.Context, job taskrt.AgentJob) error {
	jobs, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return fmt.Errorf("wrapped task runtime does not implement JobRuntime")
	}
	return jobs.UpsertJob(ctx, job)
}

func (r *eventedTaskRuntime) GetJob(ctx context.Context, id string) (*taskrt.AgentJob, error) {
	jobs, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement JobRuntime")
	}
	return jobs.GetJob(ctx, id)
}

func (r *eventedTaskRuntime) ListJobs(ctx context.Context, query taskrt.JobQuery) ([]taskrt.AgentJob, error) {
	jobs, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement JobRuntime")
	}
	return jobs.ListJobs(ctx, query)
}

func (r *eventedTaskRuntime) UpsertJobItem(ctx context.Context, item taskrt.AgentJobItem) error {
	jobs, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return fmt.Errorf("wrapped task runtime does not implement JobRuntime")
	}
	return jobs.UpsertJobItem(ctx, item)
}

func (r *eventedTaskRuntime) ListJobItems(ctx context.Context, query taskrt.JobItemQuery) ([]taskrt.AgentJobItem, error) {
	jobs, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement JobRuntime")
	}
	return jobs.ListJobItems(ctx, query)
}

func (r *eventedTaskRuntime) MarkJobItemRunning(ctx context.Context, jobID, itemID, executor string) (*taskrt.AgentJobItem, error) {
	atomic, ok := r.inner.(taskrt.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement AtomicJobRuntime")
	}
	return atomic.MarkJobItemRunning(ctx, jobID, itemID, executor)
}

func (r *eventedTaskRuntime) ReportJobItemResult(ctx context.Context, jobID, itemID, executor string, status taskrt.AgentJobStatus, result string, errMsg string) (*taskrt.AgentJobItem, error) {
	atomic, ok := r.inner.(taskrt.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("wrapped task runtime does not implement AtomicJobRuntime")
	}
	return atomic.ReportJobItemResult(ctx, jobID, itemID, executor, status, result, errMsg)
}

type eventedArtifactStore struct {
	inner   artifact.Store
	emitter *swarmEventEmitter
}

func (s *eventedArtifactStore) swarmWrapped() {}

func WrapArtifactStore(store artifact.Store, emitter *swarmEventEmitter) artifact.Store {
	if store == nil || emitter == nil {
		return store
	}
	if _, ok := store.(swarmWrappedMarker); ok {
		return store
	}
	return &eventedArtifactStore{inner: store, emitter: emitter}
}

func (s *eventedArtifactStore) Save(ctx context.Context, sessionID string, item *artifact.Artifact) error {
	if err := s.inner.Save(ctx, sessionID, item); err != nil {
		return err
	}
	s.emitter.emitArtifact(ctx, sessionID, item)
	return nil
}

func (s *eventedArtifactStore) Load(ctx context.Context, sessionID, name string, version int) (*artifact.Artifact, error) {
	return s.inner.Load(ctx, sessionID, name, version)
}

func (s *eventedArtifactStore) List(ctx context.Context, sessionID string) ([]*artifact.Artifact, error) {
	return s.inner.List(ctx, sessionID)
}

func (s *eventedArtifactStore) Versions(ctx context.Context, sessionID, name string) ([]*artifact.Artifact, error) {
	return s.inner.Versions(ctx, sessionID, name)
}

func (s *eventedArtifactStore) Delete(ctx context.Context, sessionID, name string) error {
	return s.inner.Delete(ctx, sessionID, name)
}

func (e *swarmEventEmitter) emitSession(ctx context.Context, before, current *session.Session) {
	if current == nil {
		return
	}
	_, parentID, taskID, runID, role, preview, _, _, _ := session.ThreadMetadataValues(current)
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	rootSessionID := e.rootSessionID(ctx, runID, current.ID)
	if before == nil && strings.TrimSpace(parentID) == "" {
		now := e.eventTime(current.CreatedAt)
		e.emit(ctx, rootSessionID, observe.ExecutionEvent{
			Type:        observe.ExecutionSwarmStarted,
			SessionID:   rootSessionID,
			Timestamp:   now,
			Phase:       "swarm",
			Actor:       strings.TrimSpace(role),
			PayloadKind: "swarm",
			Metadata: map[string]any{
				"swarm_run_id":   runID,
				"root_thread_id": current.ID,
				"role":           role,
				"goal":           current.Config.Goal,
			},
		}, kruntime.RuntimeEvent{
			Type:      kruntime.EventTypeSwarmStarted,
			Timestamp: now,
			Payload: &kruntime.SwarmStartedPayload{
				SwarmRunID:   runID,
				Goal:         strings.TrimSpace(current.Config.Goal),
				RootThreadID: current.ID,
				Roles:        compactStrings([]string{role}),
			},
		})
	}
	if before == nil {
		now := e.eventTime(current.CreatedAt)
		e.emit(ctx, rootSessionID, observe.ExecutionEvent{
			Type:        observe.ExecutionSwarmThreadSpawned,
			SessionID:   rootSessionID,
			Timestamp:   now,
			Phase:       "swarm",
			Actor:       strings.TrimSpace(role),
			PayloadKind: "swarm",
			Metadata: map[string]any{
				"swarm_run_id":     runID,
				"thread_id":        current.ID,
				"parent_thread_id": parentID,
				"task_id":          taskID,
				"session_id":       current.ID,
				"role":             role,
				"goal":             current.Config.Goal,
			},
		}, kruntime.RuntimeEvent{
			Type:      kruntime.EventTypeSwarmThreadSpawned,
			Timestamp: now,
			Payload: &kruntime.SwarmThreadSpawnedPayload{
				SwarmRunID:     runID,
				ThreadID:       current.ID,
				ParentThreadID: parentID,
				TaskID:         taskID,
				SessionID:      current.ID,
				Role:           role,
				Goal:           strings.TrimSpace(current.Config.Goal),
			},
		})
	}
	if before != nil && before.Status != current.Status && (current.Status == session.StatusCompleted || current.Status == session.StatusFailed) {
		now := e.eventTime(current.EndedAt)
		e.emit(ctx, rootSessionID, observe.ExecutionEvent{
			Type:        observe.ExecutionSwarmThreadDone,
			SessionID:   rootSessionID,
			Timestamp:   now,
			Phase:       "swarm",
			Actor:       strings.TrimSpace(role),
			PayloadKind: "swarm",
			Metadata: map[string]any{
				"swarm_run_id": runID,
				"thread_id":    current.ID,
				"status":       string(current.Status),
				"summary":      preview,
			},
		}, kruntime.RuntimeEvent{
			Type:      kruntime.EventTypeSwarmThreadCompleted,
			Timestamp: now,
			Payload: &kruntime.SwarmThreadCompletedPayload{
				SwarmRunID: runID,
				ThreadID:   current.ID,
				Status:     string(current.Status),
				Summary:    preview,
			},
		})
		if strings.TrimSpace(parentID) == "" {
			execType := observe.ExecutionSwarmCompleted
			runtimeType := kruntime.EventTypeSwarmCompleted
			var payload any = &kruntime.SwarmCompletedPayload{
				SwarmRunID: runID,
				Summary:    preview,
			}
			meta := map[string]any{
				"swarm_run_id": runID,
				"summary":      preview,
			}
			if current.Status == session.StatusFailed {
				execType = observe.ExecutionSwarmFailed
				runtimeType = kruntime.EventTypeSwarmFailed
				payload = &kruntime.SwarmFailedPayload{
					SwarmRunID:     runID,
					ErrorMessage:   preview,
					FailedThreadID: current.ID,
				}
				meta["thread_id"] = current.ID
			}
			e.emit(ctx, rootSessionID, observe.ExecutionEvent{
				Type:        execType,
				SessionID:   rootSessionID,
				Timestamp:   now,
				Phase:       "swarm",
				Actor:       strings.TrimSpace(role),
				PayloadKind: "swarm",
				Metadata:    meta,
			}, kruntime.RuntimeEvent{
				Type:      runtimeType,
				Timestamp: now,
				Payload:   payload,
			})
		}
	}
}

func (e *swarmEventEmitter) emitTask(ctx context.Context, before *taskrt.TaskRecord, current taskrt.TaskRecord) {
	runID := strings.TrimSpace(current.SwarmRunID)
	if runID == "" {
		return
	}
	rootSessionID := e.rootSessionID(ctx, runID, firstNonEmpty(current.SessionID, current.ThreadID))
	now := e.eventTime(current.UpdatedAt)
	if before == nil {
		e.emit(ctx, rootSessionID, observe.ExecutionEvent{
			Type:        observe.ExecutionSwarmTaskCreated,
			SessionID:   rootSessionID,
			Timestamp:   now,
			Phase:       "swarm",
			Actor:       strings.TrimSpace(current.AgentName),
			PayloadKind: "swarm",
			Metadata: map[string]any{
				"swarm_run_id": runID,
				"thread_id":    current.ThreadID,
				"task_id":      current.ID,
				"goal":         current.Goal,
				"depends_on":   append([]string(nil), current.DependsOn...),
			},
		}, kruntime.RuntimeEvent{
			Type:      kruntime.EventTypeSwarmTaskCreated,
			Timestamp: now,
			Payload: &kruntime.SwarmTaskCreatedPayload{
				SwarmRunID: runID,
				ThreadID:   strings.TrimSpace(current.ThreadID),
				TaskID:     strings.TrimSpace(current.ID),
				Goal:       strings.TrimSpace(current.Goal),
				DependsOn:  append([]string(nil), current.DependsOn...),
			},
		})
	}
	if before != nil && before.ClaimedBy != current.ClaimedBy && strings.TrimSpace(current.ClaimedBy) != "" {
		e.emit(ctx, rootSessionID, observe.ExecutionEvent{
			Type:        observe.ExecutionSwarmTaskClaimed,
			SessionID:   rootSessionID,
			Timestamp:   now,
			Phase:       "swarm",
			Actor:       strings.TrimSpace(current.ClaimedBy),
			PayloadKind: "swarm",
			Metadata: map[string]any{
				"swarm_run_id":         runID,
				"thread_id":            current.ThreadID,
				"task_id":              current.ID,
				"claimed_by_thread_id": current.ClaimedBy,
			},
		}, kruntime.RuntimeEvent{
			Type:      kruntime.EventTypeSwarmTaskClaimed,
			Timestamp: now,
			Payload: &kruntime.SwarmTaskClaimedPayload{
				SwarmRunID:        runID,
				ThreadID:          strings.TrimSpace(current.ThreadID),
				TaskID:            strings.TrimSpace(current.ID),
				ClaimedByThreadID: strings.TrimSpace(current.ClaimedBy),
			},
		})
	}
}

func (e *swarmEventEmitter) emitMessage(ctx context.Context, message taskrt.TaskMessage) {
	runID := strings.TrimSpace(message.SwarmRunID)
	if runID == "" {
		return
	}
	rootSessionID := e.rootSessionID(ctx, runID, firstNonEmpty(message.ThreadID, message.ToThreadID, message.FromThreadID))
	now := e.eventTime(message.CreatedAt)
	meta := map[string]any{
		"swarm_run_id":   runID,
		"thread_id":      message.ThreadID,
		"message_id":     message.ID,
		"from_thread_id": message.FromThreadID,
		"to_thread_id":   message.ToThreadID,
		"task_id":        message.TaskID,
		"kind":           message.Kind,
		"subject":        message.Subject,
	}
	if action := string(kswarm.GovernanceActionFromMetadata(message.Metadata)); action != "" {
		meta["governance_action"] = action
	}
	if reason, _ := message.Metadata[kswarm.MetadataGovernanceReason].(string); strings.TrimSpace(reason) != "" {
		meta["governance_reason"] = reason
	}
	e.emit(ctx, rootSessionID, observe.ExecutionEvent{
		Type:        observe.ExecutionSwarmMessageSent,
		SessionID:   rootSessionID,
		Timestamp:   now,
		Phase:       "swarm",
		Actor:       strings.TrimSpace(message.FromThreadID),
		PayloadKind: "swarm",
		Metadata:    meta,
	}, kruntime.RuntimeEvent{
		Type:      kruntime.EventTypeSwarmMessageSent,
		Timestamp: now,
		Payload: &kruntime.SwarmMessageSentPayload{
			SwarmRunID:   runID,
			ThreadID:     strings.TrimSpace(message.ThreadID),
			MessageID:    strings.TrimSpace(message.ID),
			FromThreadID: strings.TrimSpace(message.FromThreadID),
			ToThreadID:   strings.TrimSpace(message.ToThreadID),
			TaskID:       strings.TrimSpace(message.TaskID),
			Kind:         strings.TrimSpace(message.Kind),
			Subject:      strings.TrimSpace(message.Subject),
		},
	})
}

func (e *swarmEventEmitter) emitArtifact(ctx context.Context, sessionID string, item *artifact.Artifact) {
	ref, err := kswarm.ArtifactRefFromArtifact(sessionID, item)
	if err != nil || strings.TrimSpace(ref.RunID) == "" {
		return
	}
	rootSessionID := e.rootSessionID(ctx, ref.RunID, sessionID)
	now := e.eventTime(ref.CreatedAt)
	e.emit(ctx, rootSessionID, observe.ExecutionEvent{
		Type:        observe.ExecutionSwarmArtifactPub,
		SessionID:   rootSessionID,
		Timestamp:   now,
		Phase:       "swarm",
		Actor:       strings.TrimSpace(ref.ThreadID),
		PayloadKind: "swarm",
		Metadata: map[string]any{
			"swarm_run_id":  ref.RunID,
			"thread_id":     ref.ThreadID,
			"task_id":       ref.TaskID,
			"artifact_id":   ref.ID,
			"artifact_name": ref.Name,
			"artifact_kind": string(ref.Kind),
			"version":       ref.Version,
			"session_id":    sessionID,
		},
	}, kruntime.RuntimeEvent{
		Type:      kruntime.EventTypeSwarmArtifactPublished,
		Timestamp: now,
		Payload: &kruntime.SwarmArtifactPublishedPayload{
			SwarmRunID: ref.RunID,
			ThreadID:   strings.TrimSpace(ref.ThreadID),
			TaskID:     strings.TrimSpace(ref.TaskID),
			ArtifactID: strings.TrimSpace(ref.ID),
			SessionID:  strings.TrimSpace(sessionID),
			Name:       strings.TrimSpace(ref.Name),
			Kind:       string(ref.Kind),
			Version:    ref.Version,
		},
	})
}

func (e *swarmEventEmitter) emit(ctx context.Context, sessionID string, execution observe.ExecutionEvent, runtimeEvent kruntime.RuntimeEvent) {
	if e == nil || e.k == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	execution.SessionID = sessionID
	execution.Timestamp = e.eventTime(execution.Timestamp)
	e.k.Observer().OnExecutionEvent(ctx, execution)
	runtimeEvent.SessionID = sessionID
	runtimeEvent.Timestamp = e.eventTime(runtimeEvent.Timestamp)
	e.appendRuntimeEvent(ctx, sessionID, runtimeEvent)
}

func (e *swarmEventEmitter) appendRuntimeEvent(ctx context.Context, sessionID string, event kruntime.RuntimeEvent) {
	store := e.k.EventStore()
	if store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	expectedSeq := int64(0)
	view, err := store.LoadSessionView(ctx, sessionID)
	if err == nil && view != nil {
		expectedSeq = view.CurrentSeq
	} else if err != nil && !errors.Is(err, kruntime.ErrSessionNotFound) {
		e.warn(ctx, "load runtime session for swarm event failed", "session_id", sessionID, "type", event.Type, "error", err)
		return
	}
	if err := store.AppendEvents(ctx, sessionID, expectedSeq, "", []kruntime.RuntimeEvent{event}); err != nil {
		e.warn(ctx, "append swarm runtime event failed", "session_id", sessionID, "type", event.Type, "error", err)
	}
}

func (e *swarmEventEmitter) rootSessionID(ctx context.Context, runID, fallback string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return strings.TrimSpace(fallback)
	}
	catalog := session.Catalog{Store: e.k.SessionStore(), Checkpoints: e.k.Checkpoints()}
	threads, err := catalog.ListThreads(ctx, session.ThreadQuery{SwarmRunID: runID, IncludeArchived: true})
	if err == nil {
		for _, thread := range threads {
			if strings.TrimSpace(thread.ParentSessionID) == "" && strings.TrimSpace(thread.SessionID) != "" {
				return strings.TrimSpace(thread.SessionID)
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func (e *swarmEventEmitter) eventTime(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}

func (e *swarmEventEmitter) warn(ctx context.Context, msg string, args ...any) {
	if e == nil || e.k == nil || e.k.Logger() == nil {
		return
	}
	e.k.Logger().WarnContext(ctx, msg, args...)
}

func compactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
