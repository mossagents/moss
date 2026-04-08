package workspace

import (
	"context"
	"errors"
	"time"
)

// WorktreeSnapshotMode 表示快照形态。
type WorktreeSnapshotMode string

const (
	WorktreeSnapshotGhostState WorktreeSnapshotMode = "ghost-state"
)

// PatchSnapshotRef 是快照中记录的补丁引用。
type PatchSnapshotRef struct {
	PatchID     string      `json:"patch_id"`
	TargetFiles []string    `json:"target_files,omitempty"`
	Source      PatchSource `json:"source,omitempty"`
	AppliedAt   time.Time   `json:"applied_at,omitempty"`
}

// WorktreeSnapshot 描述一次 ghost-state 或真实 worktree 快照。
type WorktreeSnapshot struct {
	ID        string               `json:"id"`
	SessionID string               `json:"session_id,omitempty"`
	Mode      WorktreeSnapshotMode `json:"mode"`
	RepoRoot  string               `json:"repo_root"`
	Note      string               `json:"note,omitempty"`
	Capture   RepoState            `json:"capture"`
	Patches   []PatchSnapshotRef   `json:"patches,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
}

// WorktreeSnapshotRequest 描述创建快照的请求。
type WorktreeSnapshotRequest struct {
	SessionID string     `json:"session_id,omitempty"`
	Capture   *RepoState `json:"capture,omitempty"`
	Note      string     `json:"note,omitempty"`
}

// WorktreeSnapshotStore 提供 worktree/ghost-state 快照能力。
type WorktreeSnapshotStore interface {
	Create(ctx context.Context, req WorktreeSnapshotRequest) (*WorktreeSnapshot, error)
	Load(ctx context.Context, id string) (*WorktreeSnapshot, error)
	List(ctx context.Context) ([]WorktreeSnapshot, error)
	FindBySession(ctx context.Context, sessionID string) ([]WorktreeSnapshot, error)
}

var (
	ErrWorktreeSnapshotUnavailable = errors.New("worktree snapshot is unavailable")
	ErrWorktreeSnapshotNotFound    = errors.New("worktree snapshot not found")
)
