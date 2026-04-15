package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/internal/stringutil"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/memory"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	memstore "github.com/mossagents/moss/runtime/memory"
)

func StateEntryFromSession(sess *session.Session) (StateEntry, bool) {
	if sess == nil || !session.VisibleInHistory(sess) {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(sess.Config.Goal)
	if title == "" {
		title = sess.ID
	}
	source, parentID, taskID, preview, activityKind, archived, activityAt := session.ThreadMetadataValues(sess)
	sortTime := sessionSortTime(sess)
	if !activityAt.IsZero() {
		sortTime = activityAt.UTC()
	}
	return StateEntry{
		Kind:       StateKindSession,
		RecordID:   sess.ID,
		SessionID:  sess.ID,
		Status:     string(sess.Status),
		Title:      title,
		Summary:    stringutil.FirstNonEmpty(strings.TrimSpace(preview), strings.TrimSpace(sess.Config.Mode)),
		SearchText: normalizeStateText(sess.ID, sess.Config.Goal, sess.Config.Mode, string(sess.Status), source, parentID, taskID, preview, activityKind),
		SortTime:   sortTime,
		CreatedAt:  sess.CreatedAt,
		UpdatedAt:  sortTime,
		Metadata: marshalStateMetadata(map[string]any{
			"mode":        sess.Config.Mode,
			"recoverable": session.IsRecoverableStatus(sess.Status),
			"steps":       sess.Budget.UsedSteps,
			"source":      source,
			"parent_id":   parentID,
			"task_id":     taskID,
			"preview":     preview,
			"archived":    archived,
			"activity":    activityKind,
		}),
	}, true
}

func sessionSortTime(sess *session.Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	if !sess.EndedAt.IsZero() {
		return sess.EndedAt.UTC()
	}
	if !sess.CreatedAt.IsZero() {
		return sess.CreatedAt.UTC()
	}
	return time.Now().UTC()
}

func LogicalCheckpointSessionID(item *checkpoint.CheckpointRecord) string {
	if item == nil {
		return ""
	}
	sessionID := strings.TrimSpace(item.SessionID)
	for _, ref := range item.Lineage {
		if ref.Kind == checkpoint.CheckpointLineageSession && strings.TrimSpace(ref.ID) != "" {
			sessionID = strings.TrimSpace(ref.ID)
			break
		}
	}
	return sessionID
}

func StateEntryFromCheckpoint(item *checkpoint.CheckpointRecord) (StateEntry, bool) {
	if item == nil {
		return StateEntry{}, false
	}
	sessionID := LogicalCheckpointSessionID(item)
	return StateEntry{
		Kind:       StateKindCheckpoint,
		RecordID:   item.ID,
		SessionID:  sessionID,
		Status:     "created",
		Title:      stringutil.FirstNonEmpty(strings.TrimSpace(item.Note), item.ID),
		Summary:    fmt.Sprintf("patches=%d lineage=%d", len(item.PatchIDs), len(item.Lineage)),
		SearchText: normalizeStateText(item.ID, sessionID, item.Note),
		SortTime:   item.CreatedAt.UTC(),
		CreatedAt:  item.CreatedAt.UTC(),
		UpdatedAt:  item.CreatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"lineage_depth":        len(item.Lineage),
			"patch_count":          len(item.PatchIDs),
			"worktree_snapshot_id": item.WorktreeSnapshotID,
		}),
	}, true
}

func StateEntryFromTask(task taskrt.TaskRecord) (StateEntry, bool) {
	if strings.TrimSpace(task.ID) == "" {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(task.Goal)
	if title == "" {
		title = task.ID
	}
	sortTime := task.UpdatedAt
	if sortTime.IsZero() {
		sortTime = task.CreatedAt
	}
	return StateEntry{
		Kind:       StateKindTask,
		RecordID:   task.ID,
		Workspace:  strings.TrimSpace(task.WorkspaceID),
		SessionID:  strings.TrimSpace(task.SessionID),
		Status:     string(task.Status),
		Title:      title,
		Summary:    strings.TrimSpace(task.AgentName),
		SearchText: normalizeStateText(task.ID, task.AgentName, task.Goal, task.Result, task.Error, string(task.Status), task.SessionID, task.ParentSessionID, task.JobID, task.JobItemID),
		SortTime:   sortTime.UTC(),
		CreatedAt:  task.CreatedAt.UTC(),
		UpdatedAt:  task.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"agent_name":        task.AgentName,
			"claimed_by":        task.ClaimedBy,
			"depends_on":        append([]string(nil), task.DependsOn...),
			"result":            task.Result,
			"error":             task.Error,
			"workspace_id":      task.WorkspaceID,
			"session_id":        task.SessionID,
			"parent_session_id": task.ParentSessionID,
			"job_id":            task.JobID,
			"job_item_id":       task.JobItemID,
		}),
	}, true
}

func StateEntryFromJob(job taskrt.AgentJob) (StateEntry, bool) {
	if strings.TrimSpace(job.ID) == "" {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(job.Goal)
	if title == "" {
		title = job.ID
	}
	sortTime := job.UpdatedAt
	if sortTime.IsZero() {
		sortTime = job.CreatedAt
	}
	return StateEntry{
		Kind:       StateKindJob,
		RecordID:   job.ID,
		Status:     string(job.Status),
		Title:      title,
		Summary:    strings.TrimSpace(job.AgentName),
		SearchText: normalizeStateText(job.ID, job.AgentName, job.Goal, string(job.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  job.CreatedAt.UTC(),
		UpdatedAt:  job.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"agent_name": job.AgentName,
			"revision":   job.Revision,
		}),
	}, true
}

func StateEntryFromJobItem(item taskrt.AgentJobItem) (StateEntry, bool) {
	if strings.TrimSpace(item.JobID) == "" || strings.TrimSpace(item.ItemID) == "" {
		return StateEntry{}, false
	}
	sortTime := item.UpdatedAt
	if sortTime.IsZero() {
		sortTime = item.CreatedAt
	}
	recordID := strings.TrimSpace(item.JobID) + ":" + strings.TrimSpace(item.ItemID)
	return StateEntry{
		Kind:       StateKindJobItem,
		RecordID:   recordID,
		Status:     string(item.Status),
		Title:      stringutil.FirstNonEmpty(item.ItemID, recordID),
		Summary:    strings.TrimSpace(item.Executor),
		SearchText: normalizeStateText(item.JobID, item.ItemID, item.Executor, item.Result, item.Error, string(item.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  item.CreatedAt.UTC(),
		UpdatedAt:  item.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"job_id":   item.JobID,
			"item_id":  item.ItemID,
			"executor": item.Executor,
			"result":   item.Result,
			"error":    item.Error,
		}),
	}, true
}

func StateEntryFromMemory(record memory.MemoryRecord) (StateEntry, bool) {
	if strings.TrimSpace(record.Path) == "" {
		return StateEntry{}, false
	}
	sortTime := record.UpdatedAt
	if sortTime.IsZero() {
		sortTime = memstore.MemoryFreshness(record)
	}
	return StateEntry{
		Kind:       StateKindMemory,
		RecordID:   strings.TrimSpace(record.Path),
		Workspace:  strings.TrimSpace(record.Workspace),
		RepoRoot:   strings.TrimSpace(record.CWD),
		Status:     stringutil.FirstNonEmpty(string(record.Status), string(memory.MemoryStatusActive)),
		Title:      stringutil.FirstNonEmpty(record.Path, record.Group, record.SourcePath, record.ID),
		Summary:    strings.TrimSpace(record.Summary),
		SearchText: normalizeStateText(record.Path, record.Group, record.Summary, record.Content, strings.Join(record.Tags, " "), record.SourcePath, record.CWD, record.GitBranch, record.SourceKind, string(record.Stage), string(record.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  record.CreatedAt.UTC(),
		UpdatedAt:  record.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"id":                record.ID,
			"path":              record.Path,
			"group":             record.Group,
			"stage":             record.Stage,
			"status":            record.Status,
			"tags":              append([]string(nil), record.Tags...),
			"workspace":         record.Workspace,
			"cwd":               record.CWD,
			"git_branch":        record.GitBranch,
			"source_kind":       record.SourceKind,
			"source_id":         record.SourceID,
			"source_path":       record.SourcePath,
			"source_updated_at": record.SourceUpdatedAt,
			"usage_count":       record.UsageCount,
			"last_used_at":      record.LastUsedAt,
			"citation":          record.Citation,
		}),
	}, true
}

type indexedSessionStore struct {
	inner   session.SessionStore
	catalog *StateCatalog
}

func WrapSessionStore(store session.SessionStore, catalog *StateCatalog) session.SessionStore {
	if store == nil || catalog == nil || !catalog.Enabled() {
		return store
	}
	return &indexedSessionStore{inner: store, catalog: catalog}
}

func (s *indexedSessionStore) Save(ctx context.Context, sess *session.Session) error {
	if err := s.inner.Save(ctx, sess); err != nil {
		return err
	}
	if entry, ok := StateEntryFromSession(sess); ok {
		s.catalog.BestEffortUpsert(entry)
	} else if sess != nil {
		s.catalog.BestEffortDelete(StateKindSession, sess.ID)
	}
	return nil
}

func (s *indexedSessionStore) Load(ctx context.Context, id string) (*session.Session, error) {
	return s.inner.Load(ctx, id)
}

func (s *indexedSessionStore) List(ctx context.Context) ([]session.SessionSummary, error) {
	return s.inner.List(ctx)
}

func (s *indexedSessionStore) Delete(ctx context.Context, id string) error {
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.catalog.BestEffortDelete(StateKindSession, id)
	return nil
}

func (s *indexedSessionStore) Watch(ctx context.Context, id string) (<-chan *session.Session, error) {
	watchable, ok := s.inner.(session.WatchableSessionStore)
	if !ok {
		return nil, session.ErrNotSupported
	}
	return watchable.Watch(ctx, id)
}

type indexedCheckpointStore struct {
	inner   checkpoint.CheckpointStore
	catalog *StateCatalog
}

func WrapCheckpointStore(store checkpoint.CheckpointStore, catalog *StateCatalog) checkpoint.CheckpointStore {
	if store == nil || catalog == nil || !catalog.Enabled() {
		return store
	}
	return &indexedCheckpointStore{inner: store, catalog: catalog}
}

func (s *indexedCheckpointStore) Create(ctx context.Context, req checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
	record, err := s.inner.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromCheckpoint(record); ok {
		s.catalog.BestEffortUpsert(entry)
	}
	return record, nil
}

func (s *indexedCheckpointStore) Load(ctx context.Context, id string) (*checkpoint.CheckpointRecord, error) {
	return s.inner.Load(ctx, id)
}

func (s *indexedCheckpointStore) List(ctx context.Context) ([]checkpoint.CheckpointRecord, error) {
	return s.inner.List(ctx)
}

func (s *indexedCheckpointStore) FindBySession(ctx context.Context, sessionID string) ([]checkpoint.CheckpointRecord, error) {
	return s.inner.FindBySession(ctx, sessionID)
}

type indexedTaskRuntime struct {
	inner   taskrt.TaskRuntime
	catalog *StateCatalog
}

func WrapTaskRuntime(runtime taskrt.TaskRuntime, catalog *StateCatalog) taskrt.TaskRuntime {
	if runtime == nil || catalog == nil || !catalog.Enabled() {
		return runtime
	}
	return &indexedTaskRuntime{inner: runtime, catalog: catalog}
}

func (r *indexedTaskRuntime) UpsertTask(ctx context.Context, task taskrt.TaskRecord) error {
	if err := r.inner.UpsertTask(ctx, task); err != nil {
		return err
	}
	if entry, ok := StateEntryFromTask(task); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) GetTask(ctx context.Context, id string) (*taskrt.TaskRecord, error) {
	return r.inner.GetTask(ctx, id)
}

func (r *indexedTaskRuntime) ListTasks(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskRecord, error) {
	return r.inner.ListTasks(ctx, query)
}

func (r *indexedTaskRuntime) ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*taskrt.TaskRecord, error) {
	task, err := r.inner.ClaimNextReady(ctx, claimer, preferredAgent)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromTask(*task); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return task, nil
}

func (r *indexedTaskRuntime) ListTaskSummaries(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskSummary, error) {
	if graph, ok := r.inner.(taskrt.TaskGraphRuntime); ok {
		return graph.ListTaskSummaries(ctx, query)
	}
	tasks, err := r.inner.ListTasks(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]taskrt.TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskrt.TaskSummaryFromRecord(task))
	}
	return out, nil
}

func (r *indexedTaskRuntime) ListTaskRelations(ctx context.Context, taskID string) ([]taskrt.TaskRelation, error) {
	if graph, ok := r.inner.(taskrt.TaskGraphRuntime); ok {
		return graph.ListTaskRelations(ctx, taskID)
	}
	task, err := r.inner.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return taskrt.TaskRelationsFromRecord(*task), nil
}

func (r *indexedTaskRuntime) UpsertJob(ctx context.Context, job taskrt.AgentJob) error {
	jobRuntime, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	if err := jobRuntime.UpsertJob(ctx, job); err != nil {
		return err
	}
	if entry, ok := StateEntryFromJob(job); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) GetJob(ctx context.Context, id string) (*taskrt.AgentJob, error) {
	jobRuntime, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.GetJob(ctx, id)
}

func (r *indexedTaskRuntime) ListJobs(ctx context.Context, query taskrt.JobQuery) ([]taskrt.AgentJob, error) {
	jobRuntime, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.ListJobs(ctx, query)
}

func (r *indexedTaskRuntime) UpsertJobItem(ctx context.Context, item taskrt.AgentJobItem) error {
	jobRuntime, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	if err := jobRuntime.UpsertJobItem(ctx, item); err != nil {
		return err
	}
	if entry, ok := StateEntryFromJobItem(item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) ListJobItems(ctx context.Context, query taskrt.JobItemQuery) ([]taskrt.AgentJobItem, error) {
	jobRuntime, ok := r.inner.(taskrt.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.ListJobItems(ctx, query)
}

func (r *indexedTaskRuntime) MarkJobItemRunning(ctx context.Context, jobID, itemID, executor string) (*taskrt.AgentJobItem, error) {
	atomicRuntime, ok := r.inner.(taskrt.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("atomic job runtime is not supported by wrapped task runtime")
	}
	item, err := atomicRuntime.MarkJobItemRunning(ctx, jobID, itemID, executor)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromJobItem(*item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return item, nil
}

func (r *indexedTaskRuntime) ReportJobItemResult(ctx context.Context, jobID, itemID, executor string, status taskrt.AgentJobStatus, result string, errMsg string) (*taskrt.AgentJobItem, error) {
	atomicRuntime, ok := r.inner.(taskrt.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("atomic job runtime is not supported by wrapped task runtime")
	}
	item, err := atomicRuntime.ReportJobItemResult(ctx, jobID, itemID, executor, status, result, errMsg)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromJobItem(*item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return item, nil
}

