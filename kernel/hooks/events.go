package hooks

import (
	"encoding/json"

	intr "github.com/mossagents/moss/kernel/io"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// LLMEvent 携带 BeforeLLM / AfterLLM 阶段的上下文。
type LLMEvent struct {
	Session  *session.Session
	IO       intr.UserIO
	Observer kobs.Observer
}

// ToolEvent 携带 BeforeToolCall / AfterToolCall 阶段的上下文。
type ToolEvent struct {
	Session  *session.Session
	Tool     *tool.ToolSpec
	Input    json.RawMessage // BeforeToolCall 时有值
	Result   json.RawMessage // AfterToolCall 时有值
	IO       intr.UserIO
	Observer kobs.Observer
}

// SessionEvent 携带 OnSessionStart / OnSessionEnd 阶段的上下文。
type SessionEvent struct {
	Session  *session.Session
	IO       intr.UserIO
	Observer kobs.Observer
}

// ErrorEvent 携带 OnError 阶段的上下文。
type ErrorEvent struct {
	Session  *session.Session
	Error    error
	IO       intr.UserIO
	Observer kobs.Observer
}
