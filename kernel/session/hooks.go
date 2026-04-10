package session

import (
	"context"
	"encoding/json"
	"github.com/mossagents/moss/kernel/model"
	"time"
)

// LifecycleStage 表示 Session 生命周期 hook 的阶段。
type LifecycleStage string

const (
	LifecycleCreated   LifecycleStage = "created"
	LifecycleStarted   LifecycleStage = "started"
	LifecycleCompleted LifecycleStage = "completed"
	LifecycleFailed    LifecycleStage = "failed"
	LifecycleCancelled LifecycleStage = "cancelled"
)

// LifecycleResult 描述一次 Session 运行的最终结果摘要。
type LifecycleResult struct {
	Success    bool           `json:"success"`
	Output     string         `json:"output,omitempty"`
	Steps      int            `json:"steps"`
	TokensUsed model.TokenUsage `json:"tokens_used"`
	Error      string         `json:"error,omitempty"`
}

// LifecycleEvent 描述一次 Session 生命周期事件。
type LifecycleEvent struct {
	Stage     LifecycleStage   `json:"stage"`
	Session   *Session         `json:"-"`
	Result    *LifecycleResult `json:"result,omitempty"`
	Error     error            `json:"-"`
	Timestamp time.Time        `json:"timestamp"`
}

// LifecycleHook 在 Session 生命周期阶段被调用。
type LifecycleHook func(context.Context, LifecycleEvent)

// ToolLifecycleStage 表示工具调用 hook 的阶段。
type ToolLifecycleStage string

const (
	ToolLifecycleBefore ToolLifecycleStage = "before"
	ToolLifecycleAfter  ToolLifecycleStage = "after"
)

// ToolLifecycleEvent 描述一次工具调用生命周期事件。
type ToolLifecycleEvent struct {
	Stage     ToolLifecycleStage `json:"stage"`
	Session   *Session           `json:"-"`
	ToolName  string             `json:"tool_name"`
	CallID    string             `json:"call_id,omitempty"`
	Arguments json.RawMessage    `json:"arguments,omitempty"`
	Result    *model.ToolResult    `json:"result,omitempty"`
	Risk      string             `json:"risk,omitempty"`
	Duration  time.Duration      `json:"duration,omitempty"`
	Error     error              `json:"-"`
	Timestamp time.Time          `json:"timestamp"`
}

// ToolLifecycleHook 在工具调用生命周期阶段被调用。
type ToolLifecycleHook func(context.Context, ToolLifecycleEvent)
