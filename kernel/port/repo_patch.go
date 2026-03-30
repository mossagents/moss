package port

import (
	"context"
	"errors"
	"time"
)

// PatchSource 表示补丁来源。
type PatchSource string

const (
	PatchSourceLLM  PatchSource = "llm"
	PatchSourceTool PatchSource = "tool"
	PatchSourceUser PatchSource = "user"
)

// PatchApplyRequest 描述一次结构化补丁应用请求。
type PatchApplyRequest struct {
	Patch    string      `json:"patch"`
	Source   PatchSource `json:"source,omitempty"`
	ThreeWay bool        `json:"three_way,omitempty"`
	Cached   bool        `json:"cached,omitempty"`
}

// PatchApplyResult 描述补丁应用结果。
type PatchApplyResult struct {
	PatchID     string      `json:"patch_id,omitempty"`
	TargetFiles []string    `json:"target_files,omitempty"`
	Applied     bool        `json:"applied"`
	Cached      bool        `json:"cached,omitempty"`
	ThreeWay    bool        `json:"three_way,omitempty"`
	Source      PatchSource `json:"source,omitempty"`
	Error       string      `json:"error,omitempty"`
	AppliedAt   time.Time   `json:"applied_at"`
}

// PatchApply 提供结构化补丁应用能力。
type PatchApply interface {
	Apply(ctx context.Context, req PatchApplyRequest) (*PatchApplyResult, error)
}

// PatchRevertRequest 描述一次结构化补丁回滚请求。
type PatchRevertRequest struct {
	PatchID          string     `json:"patch_id,omitempty"`
	Capture          *RepoState `json:"capture,omitempty"`
	RestoreTracked   bool       `json:"restore_tracked,omitempty"`
	RestoreUntracked bool       `json:"restore_untracked,omitempty"`
}

// PatchRevertResult 描述补丁回滚结果。
type PatchRevertResult struct {
	PatchID     string    `json:"patch_id,omitempty"`
	Mode        string    `json:"mode,omitempty"`
	TargetFiles []string  `json:"target_files,omitempty"`
	Reverted    bool      `json:"reverted"`
	Error       string    `json:"error,omitempty"`
	RevertedAt  time.Time `json:"reverted_at"`
}

// PatchRevert 提供结构化补丁回滚能力。
type PatchRevert interface {
	Revert(ctx context.Context, req PatchRevertRequest) (*PatchRevertResult, error)
}

// ErrPatchApplyUnavailable 表示当前上下文不可执行补丁应用。
var ErrPatchApplyUnavailable = errors.New("patch apply is unavailable")

// ErrPatchRevertUnavailable 表示当前上下文不可执行补丁回滚。
var ErrPatchRevertUnavailable = errors.New("patch revert is unavailable")

// ErrPatchNotFound 表示无法根据 patch id 找到已记录补丁。
var ErrPatchNotFound = errors.New("patch not found")
