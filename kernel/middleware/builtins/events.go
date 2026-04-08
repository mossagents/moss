package builtins

import (
	"context"
	"github.com/mossagents/moss/kernel/middleware"
	"path"
	"time"
)

// Event 表示一个系统事件。
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// EventHandler 处理事件的回调函数。
type EventHandler func(Event)

// EventEmitter 构造事件发射 middleware，当 phase 对应的事件类型匹配 pattern 时触发 handler。
// pattern 使用 path.Match 语法（如 "tool.*"、"session.*"、"*"）。
func EventEmitter(pattern string, handler EventHandler) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		err := next(ctx)

		eventType := phaseToEventType(mc)
		if eventType == "" {
			return err
		}

		matched, _ := path.Match(pattern, eventType)
		if !matched {
			return err
		}

		handler(Event{
			Type:      eventType,
			Timestamp: time.Now(),
			Data:      buildEventData(mc, err),
		})

		return err
	}
}

func phaseToEventType(mc *middleware.Context) string {
	switch mc.Phase {
	case middleware.BeforeToolCall:
		return "tool.started"
	case middleware.AfterToolCall:
		return "tool.completed"
	case middleware.OnSessionStart:
		return "session.started"
	case middleware.OnSessionEnd:
		return "session.ended"
	case middleware.BeforeLLM:
		return "llm.started"
	case middleware.AfterLLM:
		return "llm.completed"
	case middleware.OnError:
		return "error"
	default:
		return ""
	}
}

func buildEventData(mc *middleware.Context, err error) map[string]any {
	data := map[string]any{
		"session_id": mc.Session.ID,
	}
	if mc.Tool != nil {
		data["tool"] = mc.Tool.Name
	}
	if err != nil {
		data["error"] = err.Error()
	}
	return data
}
