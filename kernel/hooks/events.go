package hooks

import (
	"encoding/json"

	"github.com/mossagents/moss/kernel/io"
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

// ToolEvent 携带 BeforeToolCall / AfterToolCall 阶段的上下文。
type ToolEvent struct {
	Session  *session.Session
	Tool     *tool.ToolSpec
	Input    json.RawMessage // BeforeToolCall 时有值
	Result   json.RawMessage // AfterToolCall 时有值
	IO       io.UserIO
	Observer observe.Observer
}

// SessionEvent 携带 OnSessionStart / OnSessionEnd 阶段的上下文。
type SessionEvent struct {
	Session  *session.Session
	IO       io.UserIO
	Observer observe.Observer
}

// ErrorEvent 携带 OnError 阶段的上下文。
type ErrorEvent struct {
	Session  *session.Session
	Error    error
	IO       io.UserIO
	Observer observe.Observer
}
