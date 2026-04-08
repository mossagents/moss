package checkpoint

import (
	"context"
	"errors"
	"time"
)

// CheckpointLineageKind 表示 checkpoint 谱系节点类型。
type CheckpointLineageKind string

const (
	CheckpointLineageSession    CheckpointLineageKind = "session"
	CheckpointLineageCheckpoint CheckpointLineageKind = "checkpoint"
	CheckpointLineageReplay     CheckpointLineageKind = "replay"
)

// CheckpointLineageRef 描述 checkpoint 的来源谱系。
type CheckpointLineageRef struct {
	Kind CheckpointLineageKind `json:"kind"`
	ID   string                `json:"id"`
}

// CheckpointRecord 描述一个可恢复的 checkpoint。
type CheckpointRecord struct {
	ID                 string                 `json:"id"`
	SessionID          string                 `json:"session_id"`
	WorktreeSnapshotID string                 `json:"worktree_snapshot_id,omitempty"`
	PatchIDs           []string               `json:"patch_ids,omitempty"`
	Lineage            []CheckpointLineageRef `json:"lineage,omitempty"`
	Note               string                 `json:"note,omitempty"`
	Metadata           map[string]any         `json:"metadata,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
}

// CheckpointCreateRequest 描述创建 checkpoint 的请求。
type CheckpointCreateRequest struct {
	SessionID          string                 `json:"session_id"`
	WorktreeSnapshotID string                 `json:"worktree_snapshot_id,omitempty"`
	PatchIDs           []string               `json:"patch_ids,omitempty"`
	Lineage            []CheckpointLineageRef `json:"lineage,omitempty"`
	Note               string                 `json:"note,omitempty"`
	Metadata           map[string]any         `json:"metadata,omitempty"`
}

// ForkSourceKind 表示 fork 的来源类型。
type ForkSourceKind string

const (
	ForkSourceCheckpoint ForkSourceKind = "checkpoint"
	ForkSourceSession    ForkSourceKind = "session"
)

// ForkRequest 描述从 checkpoint 或 session 派生新 session 的请求。
type ForkRequest struct {
	SourceKind      ForkSourceKind `json:"source_kind"`
	SourceID        string         `json:"source_id"`
	Note            string         `json:"note,omitempty"`
	RestoreWorktree bool           `json:"restore_worktree,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ReplayMode 表示 replay 的执行模式。
type ReplayMode string

const (
	ReplayModeResume ReplayMode = "resume"
	ReplayModeRerun  ReplayMode = "rerun"
)

// ReplayRequest 描述从 checkpoint 发起 replay 的请求。
type ReplayRequest struct {
	CheckpointID    string         `json:"checkpoint_id"`
	Note            string         `json:"note,omitempty"`
	RestoreWorktree bool           `json:"restore_worktree,omitempty"`
	Mode            ReplayMode     `json:"mode,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ReplayResult 描述 replay 的结构化结果。
type ReplayResult struct {
	CheckpointID     string     `json:"checkpoint_id"`
	SessionID        string     `json:"session_id,omitempty"`
	Mode             ReplayMode `json:"mode,omitempty"`
	RestoredWorktree bool       `json:"restored_worktree,omitempty"`
	Degraded         bool       `json:"degraded,omitempty"`
	Details          string     `json:"details,omitempty"`
}

// CheckpointStore 提供 checkpoint 的持久化能力。
type CheckpointStore interface {
	Create(ctx context.Context, req CheckpointCreateRequest) (*CheckpointRecord, error)
	Load(ctx context.Context, id string) (*CheckpointRecord, error)
	List(ctx context.Context) ([]CheckpointRecord, error)
	FindBySession(ctx context.Context, sessionID string) ([]CheckpointRecord, error)
}

var (
	ErrCheckpointUnavailable    = errors.New("checkpoint is unavailable")
	ErrCheckpointNotFound       = errors.New("checkpoint not found")
	ErrCheckpointNotRecoverable = errors.New("checkpoint is not recoverable")
)
