package workspace

import (
	"context"
	"errors"
	"time"
)

// RepoFileState 表示仓库中单个文件的状态。
type RepoFileState struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// RepoState 是一次结构化的仓库状态捕获。
type RepoState struct {
	RepoRoot   string          `json:"repo_root"`
	HeadSHA    string          `json:"head_sha,omitempty"`
	Branch     string          `json:"branch,omitempty"`
	Detached   bool            `json:"detached,omitempty"`
	IsDirty    bool            `json:"is_dirty"`
	Staged     []RepoFileState `json:"staged,omitempty"`
	Unstaged   []RepoFileState `json:"unstaged,omitempty"`
	Untracked  []string        `json:"untracked,omitempty"`
	Ignored    []string        `json:"ignored,omitempty"`
	CapturedAt time.Time       `json:"captured_at"`
}

// RepoStateCapture 提供仓库结构化状态捕获能力。
type RepoStateCapture interface {
	Capture(ctx context.Context) (*RepoState, error)
}

// ErrRepoUnavailable 表示当前上下文没有可捕获的 git 仓库。
var ErrRepoUnavailable = errors.New("repository state capture is unavailable")
