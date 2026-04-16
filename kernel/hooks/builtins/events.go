package builtins

import (
	"context"
	"path"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
)

// Event 表示一个系统事件。
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// EventHandler 处理事件的回调函数。
type EventHandler func(Event)

type eventEmitterPlugin struct {
	pattern string
	handler EventHandler
}

func (p *eventEmitterPlugin) Name() string { return "event-emitter" }
func (p *eventEmitterPlugin) Order() int   { return 900 }

func (p *eventEmitterPlugin) Install(reg *hooks.Registry) {
	reg.BeforeLLM.AddHook("event-emitter", func(ctx context.Context, ev *hooks.LLMEvent) error {
		emitLLMIfMatched(p.pattern, "llm.started", ev, p.handler)
		return nil
	}, 900)
	reg.AfterLLM.AddHook("event-emitter", func(ctx context.Context, ev *hooks.LLMEvent) error {
		emitLLMIfMatched(p.pattern, "llm.completed", ev, p.handler)
		return nil
	}, 900)
	reg.OnToolLifecycle.AddHook("event-emitter", func(ctx context.Context, ev *hooks.ToolEvent) error {
		emitToolIfMatched(p.pattern, ev, p.handler)
		return nil
	}, 900)
	reg.OnSessionLifecycle.AddHook("event-emitter", func(ctx context.Context, ev *session.LifecycleEvent) error {
		emitSessionIfMatched(p.pattern, ev, p.handler)
		return nil
	}, 900)
	reg.OnError.AddHook("event-emitter", func(ctx context.Context, ev *hooks.ErrorEvent) error {
		if matched, _ := path.Match(p.pattern, "error"); matched {
			data := map[string]any{}
			if ev.Session != nil {
				data["session_id"] = ev.Session.ID
			}
			if ev.Error != nil {
				data["error"] = ev.Error.Error()
			}
			p.handler(Event{
				Type:      "error",
				Timestamp: time.Now(),
				Data:      data,
			})
		}
		return nil
	}, 900)
}

// EventEmitterPlugin returns the canonical event-emitter plugin.
// pattern 使用 path.Match 语法（如 "tool.*"、"session.*"、"*"）。
func EventEmitterPlugin(pattern string, handler EventHandler) plugin.Plugin {
	return &eventEmitterPlugin{pattern: pattern, handler: handler}
}

func emitLLMIfMatched(pattern, eventType string, ev *hooks.LLMEvent, handler EventHandler) {
	matched, _ := path.Match(pattern, eventType)
	if !matched {
		return
	}
	data := map[string]any{}
	if ev.Session != nil {
		data["session_id"] = ev.Session.ID
	}
	handler(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func emitToolIfMatched(pattern string, ev *hooks.ToolEvent, handler EventHandler) {
	eventType := toolEventType(ev)
	matched, _ := path.Match(pattern, eventType)
	if !matched {
		return
	}
	data := map[string]any{}
	if ev.Session != nil {
		data["session_id"] = ev.Session.ID
	}
	if name := toolName(ev); name != "" {
		data["tool"] = name
	}
	if ev.CallID != "" {
		data["call_id"] = ev.CallID
	}
	if ev.Risk != "" {
		data["risk"] = ev.Risk
	}
	data["stage"] = string(ev.Stage)
	if ev.Duration > 0 {
		data["duration_ms"] = ev.Duration.Milliseconds()
	}
	if ev.ToolResult != nil {
		data["is_error"] = ev.ToolResult.IsError
	}
	handler(Event{
		Type:      eventType,
		Timestamp: eventTimestamp(ev.Timestamp),
		Data:      data,
	})
}

func emitSessionIfMatched(pattern string, ev *session.LifecycleEvent, handler EventHandler) {
	eventType := sessionEventType(ev)
	matched, _ := path.Match(pattern, eventType)
	if !matched {
		return
	}
	data := map[string]any{}
	if ev.Session != nil {
		data["session_id"] = ev.Session.ID
	}
	data["stage"] = string(ev.Stage)
	handler(Event{
		Type:      eventType,
		Timestamp: eventTimestamp(ev.Timestamp),
		Data:      data,
	})
}

func toolEventType(ev *hooks.ToolEvent) string {
	if ev == nil {
		return "tool.completed"
	}
	if ev.Stage == hooks.ToolLifecycleBefore {
		return "tool.started"
	}
	return "tool.completed"
}

func sessionEventType(ev *session.LifecycleEvent) string {
	if ev == nil {
		return "session.completed"
	}
	switch ev.Stage {
	case session.LifecycleCreated:
		return "session.created"
	case session.LifecycleStarted:
		return "session.started"
	case session.LifecycleFailed:
		return "session.failed"
	case session.LifecycleCancelled:
		return "session.cancelled"
	default:
		return "session.completed"
	}
}

func toolName(ev *hooks.ToolEvent) string {
	if ev == nil {
		return ""
	}
	if ev.Tool != nil && ev.Tool.Name != "" {
		return ev.Tool.Name
	}
	return ev.ToolName
}

func eventTimestamp(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now()
	}
	return ts
}
