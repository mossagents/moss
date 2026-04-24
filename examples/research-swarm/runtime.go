package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/harness/appkit"
	appconfig "github.com/mossagents/moss/harness/config"
	hswarm "github.com/mossagents/moss/harness/swarm"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	kernio "github.com/mossagents/moss/kernel/io"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

const (
	metaExecutionMode       = "example_swarm_execution_mode"
	metaRunStatus           = "example_swarm_run_status"
	metaEventsPartial       = "events_partial"
	metaEventsLastError     = "events_last_error"
	metaDegraded            = "degraded"
	metaFinalArtifactName   = "example_swarm_final_artifact_name"
	metaFinalArtifactThread = "example_swarm_final_artifact_thread"
	metaReportDetail        = "research_swarm_report_detail"
	metaReportAsOf          = "research_swarm_report_as_of"
	metaPlannerSessionID    = "example_swarm_planner_session_id"
	metaSynthSessionID      = "example_swarm_synth_session_id"
	metaReviewerSessionID   = "example_swarm_reviewer_session_id"
	metaSynthTaskID         = "example_swarm_synth_task_id"
	metaReviewTaskID        = "example_swarm_review_task_id"
	threadSourceExample     = "research-swarm-example"
	defaultLockTTL          = 5 * time.Minute
)

type storagePaths struct {
	Root        string
	Sessions    string
	Checkpoints string
	Tasks       string
	Artifacts   string
	EventsDB    string
	Exports     string
	Locks       string
}

type runtimeEnv struct {
	Paths         storagePaths
	Kernel        *kernel.Kernel
	Swarm         *hswarm.Runtime
	Orchestrator  *hswarm.ResearchOrchestrator
	SessionStore  session.SessionStore
	Catalog       session.Catalog
	TaskWriter    taskrt.TaskRuntime
	Tasks         taskrt.TaskRuntime
	Graph         taskrt.TaskGraphRuntime
	MessageWriter taskrt.TaskMessageRuntime
	Messages      taskrt.TaskMessageRuntime
	Artifacts     artifact.Store
	EventStore    kruntime.EventStore
	Targets       *TargetResolver
	Recovery      *RecoveryResolver
	Locks         *RunLockService
}

type resolvedTarget struct {
	RootSessionID    string
	SwarmRunID       string
	ResolutionSource string
}

type runSummary struct {
	RunID         string
	RootSessionID string
	Status        session.SessionStatus
	Recoverable   bool
	UpdatedAt     time.Time
}

type RecoveredRunSnapshot struct {
	RunID               string                        `json:"run_id"`
	RootSessionID       string                        `json:"root_session_id"`
	Status              string                        `json:"status"`
	ExecutionMode       string                        `json:"execution_mode"`
	ReportDetail        string                        `json:"report_detail,omitempty"`
	ReportAsOf          string                        `json:"report_as_of,omitempty"`
	Recoverable         bool                          `json:"recoverable"`
	Degraded            bool                          `json:"degraded"`
	EventsPartial       bool                          `json:"events_partial"`
	EventsLastError     string                        `json:"events_last_error,omitempty"`
	FinalArtifactName   string                        `json:"final_artifact_name,omitempty"`
	FinalArtifactThread string                        `json:"final_artifact_thread,omitempty"`
	Snapshot            *kswarm.Snapshot              `json:"snapshot,omitempty"`
	ThreadIndex         map[string]session.ThreadRef  `json:"-"`
	TaskIndex           map[string]taskrt.TaskSummary `json:"-"`
}

type TargetResolver struct {
	store    session.SessionStore
	recovery *RecoveryResolver
}

type RecoveryResolver struct {
	store   session.SessionStore
	base    kswarm.RecoveryResolver
	taskDir string
}

type RunLockService struct {
	dir string
	ttl time.Duration
	now func() time.Time
}

type RunLease struct {
	path string
}

type ErrRunLocked struct {
	RunID     string
	HeldUntil time.Time
}

func (e *ErrRunLocked) Error() string {
	return fmt.Sprintf("run %q is locked until %s", e.RunID, e.HeldUntil.UTC().Format(time.RFC3339))
}

type runLockRecord struct {
	RunID      string    `json:"run_id"`
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type reloadingMessageRuntime struct {
	dir string
}

func storageLayout() storagePaths {
	root := appconfig.AppDir()
	return storagePaths{
		Root:        root,
		Sessions:    filepath.Join(root, "sessions"),
		Checkpoints: filepath.Join(root, "checkpoints"),
		Tasks:       filepath.Join(root, "tasks"),
		Artifacts:   filepath.Join(root, "artifacts"),
		EventsDB:    filepath.Join(root, "events.db"),
		Exports:     filepath.Join(root, "exports"),
		Locks:       filepath.Join(root, "locks"),
	}
}

func buildExecutionEnv(ctx context.Context, flags *appkit.AppFlags, userIO kernio.UserIO) (*runtimeEnv, error) {
	if flags == nil {
		return nil, fmt.Errorf("app flags are required")
	}
	if userIO == nil {
		userIO = &kernio.NoOpIO{}
	}
	paths := storageLayout()
	if err := ensureDirs(paths); err != nil {
		return nil, err
	}
	cfg := appkit.NewDeepAgentConfig(
		appkit.WithDeepAgentAppName(appName),
		appkit.WithDeepAgentSessionStoreDir(paths.Sessions),
		appkit.WithDeepAgentCheckpointStoreDir(paths.Checkpoints),
		appkit.WithDeepAgentTaskRuntimeDir(paths.Tasks),
		appkit.WithDeepAgentArtifactStoreDir(paths.Artifacts),
		appkit.WithDeepAgentAdditionalFeatures(harness.EventStorePersistence(paths.EventsDB)),
		appkit.WithDeepAgentSwarm(true),
	)
	k, err := appkit.BuildDeepAgent(ctx, flags, userIO, cfg)
	if err != nil {
		return nil, err
	}
	if err := k.Boot(ctx); err != nil {
		return nil, err
	}
	rt := hswarm.RuntimeOf(k)
	if rt == nil {
		_ = k.Shutdown(ctx)
		return nil, fmt.Errorf("swarm runtime is not attached")
	}
	orch, err := rt.ResearchOrchestrator()
	if err != nil {
		_ = k.Shutdown(ctx)
		return nil, err
	}
	graph, ok := rt.Tasks.(taskrt.TaskGraphRuntime)
	if !ok {
		_ = k.Shutdown(ctx)
		return nil, fmt.Errorf("task runtime does not implement TaskGraphRuntime")
	}
	if _, ok := rt.Tasks.(taskrt.TaskMessageRuntime); !ok {
		_ = k.Shutdown(ctx)
		return nil, fmt.Errorf("task runtime does not implement TaskMessageRuntime")
	}
	rawTasks, err := taskrt.NewFileTaskRuntime(paths.Tasks)
	if err != nil {
		_ = k.Shutdown(ctx)
		return nil, err
	}
	eventStore := k.EventStore()
	if eventStore == nil {
		_ = k.Shutdown(ctx)
		return nil, fmt.Errorf("event store is not attached")
	}
	env := &runtimeEnv{
		Paths:         paths,
		Kernel:        k,
		Swarm:         rt,
		Orchestrator:  orch,
		SessionStore:  k.SessionStore(),
		Catalog:       session.Catalog{Store: k.SessionStore(), Checkpoints: k.Checkpoints()},
		TaskWriter:    rt.Tasks,
		Tasks:         rt.Tasks,
		Graph:         graph,
		MessageWriter: reloadingMessageRuntime{dir: paths.Tasks},
		Messages:      rawTasks,
		Artifacts:     rt.Artifacts,
		EventStore:    eventStore,
		Locks:         &RunLockService{dir: paths.Locks, ttl: defaultLockTTL, now: time.Now},
	}
	env.Recovery = &RecoveryResolver{
		store:   env.SessionStore,
		taskDir: paths.Tasks,
		base: kswarm.RecoveryResolver{
			Sessions:  env.Catalog,
			Tasks:     env.Graph,
			Messages:  env.Messages,
			Artifacts: env.Artifacts,
		},
	}
	env.Targets = &TargetResolver{store: env.SessionStore, recovery: env.Recovery}
	return env, nil
}

func openSnapshotEnv() (*runtimeEnv, error) {
	paths := storageLayout()
	if err := ensureDirs(paths); err != nil {
		return nil, err
	}
	store, err := session.NewFileStore(paths.Sessions)
	if err != nil {
		return nil, err
	}
	tasks, err := taskrt.NewFileTaskRuntime(paths.Tasks)
	if err != nil {
		return nil, err
	}
	artifacts, err := artifact.NewFileStore(paths.Artifacts)
	if err != nil {
		return nil, err
	}
	eventStore, err := kruntime.NewSQLiteEventStore(paths.EventsDB)
	if err != nil {
		return nil, err
	}
	env := &runtimeEnv{
		Paths:         paths,
		SessionStore:  store,
		Catalog:       session.Catalog{Store: store},
		TaskWriter:    tasks,
		Tasks:         tasks,
		Graph:         tasks,
		MessageWriter: tasks,
		Messages:      tasks,
		Artifacts:     artifacts,
		EventStore:    eventStore,
		Locks:         &RunLockService{dir: paths.Locks, ttl: defaultLockTTL, now: time.Now},
	}
	env.Recovery = &RecoveryResolver{
		store:   store,
		taskDir: paths.Tasks,
		base: kswarm.RecoveryResolver{
			Sessions:  env.Catalog,
			Tasks:     env.Graph,
			Messages:  env.Messages,
			Artifacts: env.Artifacts,
		},
	}
	env.Targets = &TargetResolver{store: store, recovery: env.Recovery}
	return env, nil
}

func (e *runtimeEnv) Close(ctx context.Context) {
	if e == nil {
		return
	}
	if e.Kernel != nil {
		_ = e.Kernel.Shutdown(ctx)
	}
	if e.EventStore != nil {
		if closer, ok := e.EventStore.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func ensureDirs(paths storagePaths) error {
	for _, dir := range []string{paths.Root, paths.Sessions, paths.Checkpoints, paths.Tasks, paths.Artifacts, paths.Exports, paths.Locks} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func (r *TargetResolver) ResolveForResume(ctx context.Context, sessionID, runID string, latest bool) (resolvedTarget, error) {
	return r.resolve(ctx, "resume", sessionID, runID, latest)
}

func (r *TargetResolver) ResolveForInspect(ctx context.Context, sessionID, runID string, latest bool) (resolvedTarget, error) {
	return r.resolve(ctx, "inspect", sessionID, runID, latest)
}

func (r *TargetResolver) ResolveForExport(ctx context.Context, sessionID, runID string, latest bool) (resolvedTarget, error) {
	return r.resolve(ctx, "export", sessionID, runID, latest)
}

func (r *TargetResolver) resolve(ctx context.Context, mode, sessionID, runID string, latest bool) (resolvedTarget, error) {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	if sessionID != "" {
		return r.resolveBySession(ctx, sessionID)
	}
	if runID != "" {
		return r.resolveByRunID(ctx, runID)
	}
	summaries, err := r.listRunSummaries(ctx)
	if err != nil {
		return resolvedTarget{}, err
	}
	if len(summaries) == 0 {
		return resolvedTarget{}, fmt.Errorf("no swarm runs found")
	}
	switch mode {
	case "resume":
		for _, item := range summaries {
			if !latest && !item.Recoverable {
				continue
			}
			snapshot, err := r.recovery.Load(ctx, resolvedTarget{RootSessionID: item.RootSessionID, SwarmRunID: item.RunID, ResolutionSource: "latest"})
			if err != nil {
				continue
			}
			if snapshot.Recoverable {
				return resolvedTarget{
					RootSessionID:    item.RootSessionID,
					SwarmRunID:       item.RunID,
					ResolutionSource: "latest-recoverable",
				}, nil
			}
		}
		return resolvedTarget{}, fmt.Errorf("no recoverable swarm runs found")
	default:
		return resolvedTarget{
			RootSessionID:    summaries[0].RootSessionID,
			SwarmRunID:       summaries[0].RunID,
			ResolutionSource: "latest",
		}, nil
	}
}

func (r *TargetResolver) resolveBySession(ctx context.Context, sessionID string) (resolvedTarget, error) {
	sess, err := r.store.Load(ctx, sessionID)
	if err != nil {
		return resolvedTarget{}, err
	}
	if sess == nil {
		return resolvedTarget{}, fmt.Errorf("session %q not found", sessionID)
	}
	runID, _ := sess.GetMetadata(session.MetadataThreadSwarmRunID)
	actual, _ := runID.(string)
	actual = strings.TrimSpace(actual)
	if actual == "" {
		return resolvedTarget{}, fmt.Errorf("session %q is not a swarm root session", sessionID)
	}
	return resolvedTarget{
		RootSessionID:    sessionID,
		SwarmRunID:       actual,
		ResolutionSource: "session",
	}, nil
}

func (r *TargetResolver) resolveByRunID(ctx context.Context, runID string) (resolvedTarget, error) {
	items, err := r.listRunSummaries(ctx)
	if err != nil {
		return resolvedTarget{}, err
	}
	for _, item := range items {
		if item.RunID != runID {
			continue
		}
		return resolvedTarget{
			RootSessionID:    item.RootSessionID,
			SwarmRunID:       item.RunID,
			ResolutionSource: "run-id",
		}, nil
	}
	return resolvedTarget{}, fmt.Errorf("swarm run %q not found", runID)
}

func (r *TargetResolver) listRunSummaries(ctx context.Context) ([]runSummary, error) {
	summaries, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]runSummary, 0, len(summaries))
	for _, item := range summaries {
		if strings.TrimSpace(item.SwarmRunID) == "" || strings.TrimSpace(item.ParentID) != "" {
			continue
		}
		out = append(out, runSummary{
			RunID:         strings.TrimSpace(item.SwarmRunID),
			RootSessionID: item.ID,
			Status:        item.Status,
			Recoverable:   item.Recoverable,
			UpdatedAt:     parseSessionTime(item.UpdatedAt, item.CreatedAt),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].RunID > out[j].RunID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (r *RecoveryResolver) Load(ctx context.Context, target resolvedTarget) (*RecoveredRunSnapshot, error) {
	resolver := r.base
	if strings.TrimSpace(r.taskDir) != "" {
		tasks, err := taskrt.NewFileTaskRuntime(r.taskDir)
		if err != nil {
			return nil, err
		}
		resolver.Tasks = tasks
		resolver.Messages = tasks
	}
	snapshot, err := resolver.LoadRun(ctx, kswarm.RecoveryQuery{
		RunID:           strings.TrimSpace(target.SwarmRunID),
		IncludeArchived: true,
	})
	if err != nil {
		return nil, err
	}
	rootID := strings.TrimSpace(target.RootSessionID)
	if rootID == "" {
		rootID = strings.TrimSpace(snapshot.RootSessionID)
	}
	root, err := r.store.Load(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("root session %q not found", rootID)
	}
	out := &RecoveredRunSnapshot{
		RunID:               strings.TrimSpace(snapshot.RunID),
		RootSessionID:       root.ID,
		Status:              string(root.Status),
		ExecutionMode:       metadataString(root, metaExecutionMode, "real"),
		ReportDetail:        metadataString(root, metaReportDetail, string(detailComprehensive)),
		ReportAsOf:          metadataString(root, metaReportAsOf, ""),
		Degraded:            metadataBool(root, metaDegraded),
		EventsPartial:       metadataBool(root, metaEventsPartial),
		EventsLastError:     metadataString(root, metaEventsLastError, ""),
		FinalArtifactName:   metadataString(root, metaFinalArtifactName, ""),
		FinalArtifactThread: metadataString(root, metaFinalArtifactThread, ""),
		Snapshot:            snapshot,
		ThreadIndex:         make(map[string]session.ThreadRef, len(snapshot.Threads)),
		TaskIndex:           make(map[string]taskrt.TaskSummary, len(snapshot.Tasks)),
	}
	for _, thread := range snapshot.Threads {
		out.ThreadIndex[thread.SessionID] = thread
	}
	for _, task := range snapshot.Tasks {
		out.TaskIndex[task.Handle.ID] = task
	}
	if out.RootSessionID == "" {
		out.Degraded = true
	}
	out.Recoverable = computeRecoverable(out.Status, snapshot.Tasks, snapshot.Messages)
	return out, nil
}

func computeRecoverable(status string, tasks []taskrt.TaskSummary, messages []taskrt.TaskMessage) bool {
	switch status {
	case string(session.StatusCompleted), string(session.StatusCancelled):
		return false
	}
	gov := latestGovernanceActions(messages)
	for _, task := range tasks {
		switch task.Status {
		case taskrt.TaskPending, taskrt.TaskRunning:
			return true
		case taskrt.TaskFailed:
			action := gov[task.Handle.ID]
			if action == kswarm.GovernanceRedirected || action == kswarm.GovernanceTakenOver {
				return true
			}
		}
	}
	return status == string(session.StatusCreated) || status == string(session.StatusRunning) || status == string(session.StatusPaused)
}

func latestGovernanceActions(messages []taskrt.TaskMessage) map[string]kswarm.GovernanceAction {
	out := make(map[string]kswarm.GovernanceAction)
	for _, message := range messages {
		action := kswarm.GovernanceActionFromMetadata(message.Metadata)
		if action == "" {
			continue
		}
		out[message.TaskID] = action
	}
	return out
}

func metadataString(sess *session.Session, key, fallback string) string {
	if sess == nil {
		return fallback
	}
	raw, ok := sess.GetMetadata(key)
	if !ok {
		return fallback
	}
	value, _ := raw.(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func metadataBool(sess *session.Session, key string) bool {
	if sess == nil {
		return false
	}
	raw, ok := sess.GetMetadata(key)
	if !ok {
		return false
	}
	value, _ := raw.(bool)
	return value
}

func parseSessionTime(updatedAt, createdAt string) time.Time {
	for _, raw := range []string{strings.TrimSpace(updatedAt), strings.TrimSpace(createdAt)} {
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts.UTC()
		}
	}
	return time.Time{}
}

func newRunLockService(dir string, ttl time.Duration, now func() time.Time) *RunLockService {
	if now == nil {
		now = time.Now
	}
	return &RunLockService{dir: dir, ttl: ttl, now: now}
}

func (s *RunLockService) Acquire(runID string) (*RunLease, error) {
	if s == nil {
		return nil, fmt.Errorf("run lock service is required")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, runID+".lock")
	now := s.now().UTC()
	record := runLockRecord{
		RunID:      runID,
		PID:        os.Getpid(),
		AcquiredAt: now,
		ExpiresAt:  now.Add(s.ttl),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			defer file.Close()
			if _, writeErr := file.Write(data); writeErr != nil {
				_ = os.Remove(path)
				return nil, writeErr
			}
			return &RunLease{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		stale, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		var existing runLockRecord
		if json.Unmarshal(stale, &existing) == nil && existing.ExpiresAt.After(now) {
			return nil, &ErrRunLocked{RunID: runID, HeldUntil: existing.ExpiresAt}
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, removeErr
		}
	}
	return nil, fmt.Errorf("failed to acquire lock for run %q", runID)
}

func (l *RunLease) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r reloadingMessageRuntime) EnqueueTaskMessage(ctx context.Context, message taskrt.TaskMessage) (*taskrt.TaskMessage, error) {
	rt, err := taskrt.NewFileTaskRuntime(r.dir)
	if err != nil {
		return nil, err
	}
	return rt.EnqueueTaskMessage(ctx, message)
}

func (r reloadingMessageRuntime) ListTaskMessages(ctx context.Context, taskID string, limit int) ([]taskrt.TaskMessage, error) {
	rt, err := taskrt.NewFileTaskRuntime(r.dir)
	if err != nil {
		return nil, err
	}
	return rt.ListTaskMessages(ctx, taskID, limit)
}

func (r reloadingMessageRuntime) ConsumeTaskMessages(ctx context.Context, taskID string, limit int) ([]taskrt.TaskMessage, error) {
	rt, err := taskrt.NewFileTaskRuntime(r.dir)
	if err != nil {
		return nil, err
	}
	return rt.ConsumeTaskMessages(ctx, taskID, limit)
}
