package port

import (
	"context"
	"errors"
)

// WorkspaceLease 表示一次隔离工作区租约。
type WorkspaceLease struct {
	WorkspaceID string    `json:"workspace_id"`
	Workspace   Workspace `json:"-"`
	Executor    Executor  `json:"-"`
}

// WorkspaceIsolation 提供按任务隔离工作区能力。
type WorkspaceIsolation interface {
	Acquire(ctx context.Context, taskID string) (*WorkspaceLease, error)
	Release(ctx context.Context, workspaceID string) error
}

// ErrIsolationUnavailable 表示未配置隔离器。
var ErrIsolationUnavailable = errors.New("workspace isolation is unavailable")

