package main

import (
	"context"
	"sync"

	"github.com/mossagents/moss/kernel/port"
)

// sessionIDKey is the context key used to pass session ID to WailsUserIO.
type sessionIDKey struct{}

// WailsUserIO 实现 port.UserIO，通过 Wails 事件系统与桌面前端通信。
type WailsUserIO struct {
	mu    sync.Mutex
	askCh chan port.InputResponse
}

var _ port.UserIO = (*WailsUserIO)(nil)

func NewWailsUserIO() *WailsUserIO {
	return &WailsUserIO{
		askCh: make(chan port.InputResponse, 1),
	}
}

func (w *WailsUserIO) Send(ctx context.Context, msg port.OutputMessage) error {
	// Session ID is carried per-run via context — no shared mutable state.
	sid, _ := ctx.Value(sessionIDKey{}).(string)

	var eventName string
	data := map[string]any{
		"content":    msg.Content,
		"meta":       msg.Meta,
		"session_id": sid,
	}

	switch msg.Type {
	case port.OutputText:
		eventName = "chat:text"
	case port.OutputStream:
		eventName = "chat:stream"
	case port.OutputStreamEnd:
		eventName = "chat:stream_end"
	case port.OutputReasoning:
		eventName = "chat:thinking"
	case port.OutputProgress:
		eventName = "chat:progress"
	case port.OutputToolStart:
		eventName = "chat:tool_start"
	case port.OutputToolResult:
		eventName = "chat:tool_result"
		if isErr, ok := msg.Meta["is_error"].(bool); ok {
			data["is_error"] = isErr
		}
	default:
		eventName = "chat:text"
	}

	emitEventOnCtx(ctx, eventName, data)
	return nil
}

func (w *WailsUserIO) Ask(ctx context.Context, req port.InputRequest) (port.InputResponse, error) {
	sid, _ := ctx.Value(sessionIDKey{}).(string)

	emitEvent("chat:ask", map[string]any{
		"type":       string(req.Type),
		"prompt":     req.Prompt,
		"options":    req.Options,
		"approval":   req.Approval,
		"meta":       req.Meta,
		"session_id": sid,
	})

	select {
	case <-ctx.Done():
		return port.InputResponse{}, ctx.Err()
	case resp := <-w.askCh:
		return resp, nil
	}
}

func (w *WailsUserIO) RespondToAsk(resp port.InputResponse) {
	select {
	case w.askCh <- resp:
	default:
	}
}
