package main

import (
	"context"
	"sync"

	"github.com/mossagi/moss/kernel/port"
)

// WailsUserIO 实现 port.UserIO，通过 Wails 事件系统与桌面前端通信。
type WailsUserIO struct {
	mu    sync.Mutex
	askCh chan port.InputResponse
}

var _ port.UserIO = (*WailsUserIO)(nil)

// NewWailsUserIO 创建 WailsUserIO 实例。
func NewWailsUserIO() *WailsUserIO {
	return &WailsUserIO{
		askCh: make(chan port.InputResponse, 1),
	}
}

// Send 将 kernel 的输出消息通过 Wails 事件推送到前端。
func (w *WailsUserIO) Send(ctx context.Context, msg port.OutputMessage) error {
	var eventName string
	data := map[string]any{
		"content": msg.Content,
		"meta":    msg.Meta,
	}

	switch msg.Type {
	case port.OutputText:
		eventName = "chat:text"
	case port.OutputStream:
		eventName = "chat:stream"
	case port.OutputStreamEnd:
		eventName = "chat:stream_end"
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

// Ask 向前端发出输入请求，阻塞等待回复。
func (w *WailsUserIO) Ask(ctx context.Context, req port.InputRequest) (port.InputResponse, error) {
	// 发射 ask 事件到前端
	emitEvent("chat:ask", map[string]any{
		"type":    string(req.Type),
		"prompt":  req.Prompt,
		"options": req.Options,
		"meta":    req.Meta,
	})

	// 阻塞等待前端回复
	select {
	case <-ctx.Done():
		return port.InputResponse{}, ctx.Err()
	case resp := <-w.askCh:
		return resp, nil
	}
}

// RespondToAsk 从前端接收 Ask 请求的回复。
func (w *WailsUserIO) RespondToAsk(resp port.InputResponse) {
	select {
	case w.askCh <- resp:
	default:
		// 没有 pending 的 ask 请求，丢弃
	}
}
