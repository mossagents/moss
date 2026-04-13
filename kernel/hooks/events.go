package hooks

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// LLMEvent 携带 BeforeLLM / AfterLLM 阶段的上下文。
type LLMEvent struct {
	Session  *session.Session
	IO       io.UserIO
	Observer observe.Observer
}

// ToolStage 表示工具调用生命周期 hook 的阶段。
type ToolStage string

const (
	ToolLifecycleBefore ToolStage = "before"
	ToolLifecycleAfter  ToolStage = "after"
)

// ToolEvent 携带工具调用生命周期阶段的上下文。
type ToolEvent struct {
	Stage      ToolStage
	Session    *session.Session
	Tool       *tool.ToolSpec
	ToolName   string
	CallID     string
	Input      json.RawMessage // 执行前将要传入工具的参数
	Output     json.RawMessage // 执行后工具返回的原始输出
	ToolResult *model.ToolResult
	Risk       string
	Duration   time.Duration
	Error      error
	Timestamp  time.Time
	IO         io.UserIO
	Observer   observe.Observer
}

// ToolHook 在工具调用生命周期阶段被调用。
type ToolHook func(context.Context, ToolEvent)

// ErrorEvent 携带 OnError 阶段的上下文。
type ErrorEvent struct {
	Session  *session.Session
	Error    error
	IO       io.UserIO
	Observer observe.Observer
}
