package main

import (
	"context"
	intr "github.com/mossagents/moss/kernel/io"
	"sync"
)

// sessionIDKey is the context key used to pass session ID to WailsUserIO.
type sessionIDKey struct{}

// WailsUserIO 实现 intr.UserIO，通过 Wails 事件系统与桌面前端通信。
type WailsUserIO struct {
	mu    sync.Mutex
	askCh chan intr.InputResponse
}

var _ intr.UserIO = (*WailsUserIO)(nil)

func NewWailsUserIO() *WailsUserIO {
	return &WailsUserIO{
		askCh: make(chan intr.InputResponse, 1),
	}
}

func (w *WailsUserIO) Send(ctx context.Context, msg intr.OutputMessage) error {
	// Session ID is carried per-run via context — no shared mutable state.
	sid, _ := ctx.Value(sessionIDKey{}).(string)

	var eventName string
	data := map[string]any{
		"content":    msg.Content,
		"meta":       msg.Meta,
		"session_id": sid,
	}

	switch msg.Type {
	case intr.OutputText:
		eventName = "chat:text"
	case intr.OutputStream:
		eventName = "chat:stream"
	case intr.OutputStreamEnd:
		eventName = "chat:stream_end"
	case intr.OutputReasoning:
		eventName = "chat:thinking"
	case intr.OutputProgress:
		eventName = "chat:progress"
	case intr.OutputToolStart:
		eventName = "chat:tool_start"
	case intr.OutputToolResult:
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

func (w *WailsUserIO) Ask(ctx context.Context, req intr.InputRequest) (intr.InputResponse, error) {
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
		return intr.InputResponse{}, ctx.Err()
	case resp := <-w.askCh:
		return resp, nil
	}
}

func (w *WailsUserIO) RespondToAsk(resp intr.InputResponse) {
	select {
	case w.askCh <- resp:
	default:
	}
}
