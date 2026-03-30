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
	PatchID     string    `json:"patch_id,omitempty"`
	TargetFiles []string  `json:"target_files,omitempty"`
	Applied     bool      `json:"applied"`
	Cached      bool      `json:"cached,omitempty"`
	ThreeWay    bool      `json:"three_way,omitempty"`
	Error       string    `json:"error,omitempty"`
	AppliedAt   time.Time `json:"applied_at"`
}

// PatchApply 提供结构化补丁应用能力。
type PatchApply interface {
	Apply(ctx context.Context, req PatchApplyRequest) (*PatchApplyResult, error)
}

// ErrPatchApplyUnavailable 表示当前上下文不可执行补丁应用。
var ErrPatchApplyUnavailable = errors.New("patch apply is unavailable")
