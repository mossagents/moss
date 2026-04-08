package middleware

import (
	"context"
	"encoding/json"
	intr "github.com/mossagents/moss/kernel/io"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// Phase 表示 Middleware 可拦截的执行阶段。
type Phase string

const (
	BeforeLLM      Phase = "before_llm"
	AfterLLM       Phase = "after_llm"
	BeforeToolCall Phase = "before_tool_call"
	AfterToolCall  Phase = "after_tool_call"
	OnSessionStart Phase = "on_session_start"
	OnSessionEnd   Phase = "on_session_end"
	OnError        Phase = "on_error"
)

// Context 携带当前执行阶段的上下文信息。
type Context struct {
	Phase    Phase
	Session  *session.Session
	Tool     *tool.ToolSpec  // 仅 tool 相关 phase
	Input    json.RawMessage // 工具输入（仅 BeforeToolCall）
	Result   json.RawMessage // 工具结果（仅 AfterToolCall）
	Error    error           // 错误信息（仅 OnError）
	IO       intr.UserIO     // 用户交互接口
	Observer kobs.Observer    // 运行事件观察者
}

// Next 调用链中的下一个 middleware。
type Next func(ctx context.Context) error

// Middleware 是统一的扩展函数签名。
type Middleware func(ctx context.Context, mc *Context, next Next) error
