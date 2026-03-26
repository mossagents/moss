package testing

import (
	"context"
	"sync"

	"github.com/mossagents/moss/kernel/port"
)

// RecorderIO 记录所有 Send/Ask 调用的 UserIO 测试桩。
type RecorderIO struct {
	mu      sync.Mutex
	Sent    []port.OutputMessage
	Asked   []port.InputRequest
	AskFunc func(port.InputRequest) (port.InputResponse, error)
}

// NewRecorderIO 创建记录器 IO。
func NewRecorderIO() *RecorderIO {
	return &RecorderIO{}
}

// Send 记录输出消息。
func (r *RecorderIO) Send(_ context.Context, msg port.OutputMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Sent = append(r.Sent, msg)
	return nil
}

// Ask 记录输入请求并返回预设响应。
func (r *RecorderIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Asked = append(r.Asked, req)
	if r.AskFunc != nil {
		return r.AskFunc(req)
	}
	// 默认批准所有 Confirm 请求
	return port.InputResponse{Approved: true}, nil
}
