package session

import (
	"context"
	"sort"
	"strings"

	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/x/stringutil"
)

// LineageKind 表示线程/检查点目录里的统一引用类型。
type LineageKind string

const (
	LineageKindSession    LineageKind = "session"
	LineageKindCheckpoint LineageKind = "checkpoint"
	LineageKindReplay     LineageKind = "replay"
	LineageKindTask       LineageKind = "task"
)

// LineageRef 描述一个可导航的谱系节点。
type LineageRef struct {
	Kind      LineageKind `json:"kind"`
	ID        string      `json:"id"`
	SessionID string      `json:"session_id,omitempty"`
	Label     string      `json:"label,omitempty"`
}

// ThreadRef 描述一个可 resume/fork 的线程摘要。
type ThreadRef struct {
	SessionID         string        `json:"session_id"`
	ParentSessionID   string        `json:"parent_session_id,omitempty"`
	TaskID            string        `json:"task_id,omitempty"`
	Goal              string        `json:"goal,omitempty"`
	Mode              string        `json:"mode,omitempty"`
	Profile           string        `json:"profile,omitempty"`
	EffectiveTrust    string        `json:"effective_trust,omitempty"`
	EffectiveApproval string        `json:"effective_approval,omitempty"`
	TaskMode          string        `json:"task_mode,omitempty"`
	Source            string        `json:"source,omitempty"`
	Preview           string        `json:"preview,omitempty"`
	ActivityKind      string        `json:"activity_kind,omitempty"`
	Status            SessionStatus `json:"status"`
	Recoverable       bool          `json:"recoverable,omitempty"`
	Archived          bool          `json:"archived,omitempty"`
	CreatedAt         string        `json:"created_at,omitempty"`
	UpdatedAt         string        `json:"updated_at,omitempty"`
	EndedAt           string        `json:"ended_at,omitempty"`
	Lineage           []LineageRef  `json:"lineage,omitempty"`
}

// CheckpointRef 描述一个可恢复的检查点摘要。
type CheckpointRef struct {
	ID                 string       `json:"id"`
	SessionID          string       `json:"session_id"`
	WorktreeSnapshotID string       `json:"worktree_snapshot_id,omitempty"`
	Note               string       `json:"note,omitempty"`
	CreatedAt          string       `json:"created_at,omitempty"`
	Lineage            []LineageRef `json:"lineage,omitempty"`
}

// ForkSource 统一表示从 session/checkpoint fork 的来源。
type ForkSource struct {
	Kind         checkpoint.ForkSourceKind `json:"kind"`
	SourceID     string                    `json:"source_id"`
	SessionID    string                    `json:"session_id,omitempty"`
	CheckpointID string                    `json:"checkpoint_id,omitempty"`
	Label        string                    `json:"label,omitempty"`
	Lineage      []LineageRef              `json:"lineage,omitempty"`
}

// ThreadQuery 用于筛选线程目录。
type ThreadQuery struct {
	SessionID       string `json:"session_id,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RecoverableOnly bool   `json:"recoverable_only,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// CheckpointQuery 用于筛选检查点目录。
type CheckpointQuery struct {
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// SessionCatalog 暴露线程/检查点目录查询能力。
type SessionCatalog interface {
	ListThreads(ctx context.Context, query ThreadQuery) ([]ThreadRef, error)
	GetThread(ctx context.Context, sessionID string) (*ThreadRef, error)
	ListCheckpoints(ctx context.Context, query CheckpointQuery) ([]CheckpointRef, error)
	ResolveForkSource(ctx context.Context, kind checkpoint.ForkSourceKind, id string) (*ForkSource, error)
}

// Catalog 使用 SessionStore/CheckpointStore 适配统一目录查询。
type Catalog struct {
	Store       SessionStore
	Checkpoints checkpoint.CheckpointStore
}

func (c Catalog) ListThreads(ctx context.Context, query ThreadQuery) ([]ThreadRef, error) {
	if c.Store == nil {
		return nil, ErrNotSupported
	}
	summaries, err := c.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadRef, 0, len(summaries))
	for _, summary := range summaries {
		if query.SessionID != "" && summary.ID != query.SessionID {
			continue
		}
		if query.ParentSessionID != "" && summary.ParentID != query.ParentSessionID {
			continue
		}
		if query.TaskID != "" && summary.TaskID != query.TaskID {
			continue
		}
		if query.RecoverableOnly && !summary.Recoverable {
			continue
		}
		if !query.IncludeArchived && summary.Archived {
			continue
		}
		out = append(out, ThreadRefFromSummary(summary))
	}
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func (c Catalog) GetThread(ctx context.Context, sessionID string) (*ThreadRef, error) {
	if c.Store == nil {
		return nil, ErrNotSupported
	}
	sess, err := c.Store.Load(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, nil
	}
	ref := ThreadRefFromSession(sess)
	return &ref, nil
}

func (c Catalog) ListCheckpoints(ctx context.Context, query CheckpointQuery) ([]CheckpointRef, error) {
	if c.Checkpoints == nil {
		return nil, ErrNotSupported
	}
	var (
		records []checkpoint.CheckpointRecord
		err     error
	)
	if strings.TrimSpace(query.SessionID) != "" {
		records, err = c.Checkpoints.FindBySession(ctx, strings.TrimSpace(query.SessionID))
	} else {
		records, err = c.Checkpoints.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointRef, 0, len(records))
	for _, record := range records {
		out = append(out, CheckpointRefFromRecord(record))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func (c Catalog) ResolveForkSource(ctx context.Context, kind checkpoint.ForkSourceKind, id string) (*ForkSource, error) {
	switch kind {
	case checkpoint.ForkSourceSession:
		thread, err := c.GetThread(ctx, id)
		if err != nil || thread == nil {
			return nil, err
		}
		return &ForkSource{
			Kind:      checkpoint.ForkSourceSession,
			SourceID:  thread.SessionID,
			SessionID: thread.SessionID,
			Label:     stringutil.FirstNonEmpty(thread.Preview, thread.Goal, thread.SessionID),
			Lineage:   append([]LineageRef(nil), thread.Lineage...),
		}, nil
	case checkpoint.ForkSourceCheckpoint:
		if c.Checkpoints == nil {
			return nil, ErrNotSupported
		}
		record, err := c.Checkpoints.Load(ctx, strings.TrimSpace(id))
		if err != nil || record == nil {
			return nil, err
		}
		ref := CheckpointRefFromRecord(*record)
		return &ForkSource{
			Kind:         checkpoint.ForkSourceCheckpoint,
			SourceID:     ref.ID,
			SessionID:    ref.SessionID,
			CheckpointID: ref.ID,
			Label:        stringutil.FirstNonEmpty(ref.Note, ref.ID),
			Lineage:      append([]LineageRef(nil), ref.Lineage...),
		}, nil
	default:
		return nil, ErrNotSupported
	}
}

func ThreadRefFromSummary(summary SessionSummary) ThreadRef {
	lineage := []LineageRef{{
		Kind:      LineageKindSession,
		ID:        summary.ID,
		SessionID: summary.ID,
		Label:     stringutil.FirstNonEmpty(summary.Preview, summary.Goal, summary.ID),
	}}
	if summary.ParentID != "" {
		lineage = append(lineage, LineageRef{
			Kind:      LineageKindSession,
			ID:        summary.ParentID,
			SessionID: summary.ParentID,
			Label:     summary.ParentID,
		})
	}
	if summary.TaskID != "" {
		lineage = append(lineage, LineageRef{
			Kind:      LineageKindTask,
			ID:        summary.TaskID,
			SessionID: summary.ID,
			Label:     summary.TaskID,
		})
	}
	return ThreadRef{
		SessionID:         summary.ID,
		ParentSessionID:   summary.ParentID,
		TaskID:            summary.TaskID,
		Goal:              summary.Goal,
		Mode:              summary.Mode,
		Profile:           summary.Profile,
		EffectiveTrust:    summary.EffectiveTrust,
		EffectiveApproval: summary.EffectiveApproval,
		TaskMode:          summary.TaskMode,
		Source:            summary.Source,
		Preview:           summary.Preview,
		ActivityKind:      summary.ActivityKind,
		Status:            summary.Status,
		Recoverable:       summary.Recoverable,
		Archived:          summary.Archived,
		CreatedAt:         summary.CreatedAt,
		UpdatedAt:         summary.UpdatedAt,
		EndedAt:           summary.EndedAt,
		Lineage:           lineage,
	}
}

func ThreadRefFromSession(sess *Session) ThreadRef {
	source, parentID, taskID, preview, activityKind, archived, activityAt := ThreadMetadataValues(sess)
	profile, effectiveTrust, effectiveApproval, taskMode := ProfileMetadataValues(sess)
	summary := SessionSummary{
		ID:                sess.ID,
		Goal:              sess.Config.Goal,
		Mode:              sess.Config.Mode,
		Profile:           profile,
		EffectiveTrust:    effectiveTrust,
		EffectiveApproval: effectiveApproval,
		TaskMode:          taskMode,
		Source:            source,
		ParentID:          parentID,
		TaskID:            taskID,
		Preview:           preview,
		ActivityKind:      activityKind,
		Status:            sess.Status,
		Recoverable:       IsRecoverableStatus(sess.Status),
		Archived:          archived,
		Steps:             sess.Budget.UsedStepsValue(),
		CreatedAt:         formatSessionTime(sess.CreatedAt),
		UpdatedAt:         formatSessionTime(activityAt),
		EndedAt:           formatSessionTime(sess.EndedAt),
	}
	return ThreadRefFromSummary(summary)
}

func CheckpointRefFromRecord(record checkpoint.CheckpointRecord) CheckpointRef {
	lineage := make([]LineageRef, 0, len(record.Lineage)+1)
	for _, ref := range record.Lineage {
		lineage = append(lineage, LineageRefFromCheckpointLineage(ref, record.SessionID))
	}
	lineage = append(lineage, LineageRef{
		Kind:      LineageKindCheckpoint,
		ID:        record.ID,
		SessionID: record.SessionID,
		Label:     stringutil.FirstNonEmpty(record.Note, record.ID),
	})
	return CheckpointRef{
		ID:                 record.ID,
		SessionID:          record.SessionID,
		WorktreeSnapshotID: record.WorktreeSnapshotID,
		Note:               record.Note,
		CreatedAt:          record.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		Lineage:            lineage,
	}
}

func LineageRefFromCheckpointLineage(ref checkpoint.CheckpointLineageRef, sessionID string) LineageRef {
	kind := LineageKindCheckpoint
	switch ref.Kind {
	case checkpoint.CheckpointLineageSession:
		kind = LineageKindSession
	case checkpoint.CheckpointLineageReplay:
		kind = LineageKindReplay
	}
	return LineageRef{
		Kind:      kind,
		ID:        ref.ID,
		SessionID: sessionID,
		Label:     ref.ID,
	}
}
