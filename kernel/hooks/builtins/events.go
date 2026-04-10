package builtins

import (
	"context"
	"path"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
)

// Event 表示一个系统事件。
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// EventHandler 处理事件的回调函数。
type EventHandler func(Event)

// InstallEventEmitter 安装事件发射 hooks，当事件类型匹配 pattern 时触发 handler。
// pattern 使用 path.Match 语法（如 "tool.*"、"session.*"、"*"）。
func InstallEventEmitter(pattern string, handler EventHandler) func(*hooks.Registry) {
	return func(reg *hooks.Registry) {
		reg.BeforeLLM.AddHook("event-emitter", func(ctx context.Context, ev *hooks.LLMEvent) error {
			emitLLMIfMatched(pattern, "llm.started", ev, handler)
			return nil
		}, 900)

		reg.AfterLLM.AddHook("event-emitter", func(ctx context.Context, ev *hooks.LLMEvent) error {
			emitLLMIfMatched(pattern, "llm.completed", ev, handler)
			return nil
		}, 900)

		reg.BeforeToolCall.AddHook("event-emitter", func(ctx context.Context, ev *hooks.ToolEvent) error {
			emitToolIfMatched(pattern, "tool.started", ev, handler)
			return nil
		}, 900)

		reg.AfterToolCall.AddHook("event-emitter", func(ctx context.Context, ev *hooks.ToolEvent) error {
			emitToolIfMatched(pattern, "tool.completed", ev, handler)
			return nil
		}, 900)

		reg.OnSessionStart.AddHook("event-emitter", func(ctx context.Context, ev *hooks.SessionEvent) error {
			emitSessionIfMatched(pattern, "session.started", ev, handler)
			return nil
		}, 900)

		reg.OnSessionEnd.AddHook("event-emitter", func(ctx context.Context, ev *hooks.SessionEvent) error {
			emitSessionIfMatched(pattern, "session.ended", ev, handler)
			return nil
		}, 900)

		reg.OnError.AddHook("event-emitter", func(ctx context.Context, ev *hooks.ErrorEvent) error {
			if matched, _ := path.Match(pattern, "error"); matched {
				data := map[string]any{}
				if ev.Session != nil {
					data["session_id"] = ev.Session.ID
				}
				if ev.Error != nil {
					data["error"] = ev.Error.Error()
				}
				handler(Event{
					Type:      "error",
					Timestamp: time.Now(),
					Data:      data,
				})
			}
			return nil
		}, 900)
	}
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

func emitToolIfMatched(pattern, eventType string, ev *hooks.ToolEvent, handler EventHandler) {
	matched, _ := path.Match(pattern, eventType)
	if !matched {
		return
	}
	data := map[string]any{}
	if ev.Session != nil {
		data["session_id"] = ev.Session.ID
	}
	if ev.Tool != nil {
		data["tool"] = ev.Tool.Name
	}
	handler(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func emitSessionIfMatched(pattern, eventType string, ev *hooks.SessionEvent, handler EventHandler) {
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
