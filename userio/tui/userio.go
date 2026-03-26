package tui

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/kernel/port"
)

// bridgeMsg 是从 UserIO 桥接到 Bubble Tea 的消息类型。
type bridgeMsg struct {
	output *port.OutputMessage
	ask    *bridgeAsk
}

type refreshMsg struct{}

// bridgeAsk 表示一个阻塞式用户输入请求。
type bridgeAsk struct {
	request port.InputRequest
	replyCh chan port.InputResponse
}

// BridgeIO 实现 port.UserIO，桥接 kernel 与 Bubble Tea TUI。
// kernel 在后台 goroutine 调用 Send/Ask，BridgeIO 将消息发送到 tea.Program。
type BridgeIO struct {
	program *tea.Program
	mu      sync.Mutex
}

// NewBridgeIO 创建桥接器。需要在 tea.Program 创建后设置 program。
func NewBridgeIO() *BridgeIO {
	return &BridgeIO{}
}

// SetProgram 设置 tea.Program 引用（需要在 Program 创建后立即调用）。
func (b *BridgeIO) SetProgram(p *tea.Program) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.program = p
}

// Send 向用户推送内容（非阻塞，通过 tea.Program.Send 注入消息）。
func (b *BridgeIO) Send(_ context.Context, msg port.OutputMessage) error {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return nil
	}
	p.Send(bridgeMsg{output: &msg})
	return nil
}

// Refresh 请求 TUI 立即重绘一次，适合外部状态面板刷新。
func (b *BridgeIO) Refresh() {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return
	}
	p.Send(refreshMsg{})
}

// Ask 向用户请求输入（阻塞当前 goroutine，等待 TUI 回复）。
func (b *BridgeIO) Ask(ctx context.Context, req port.InputRequest) (port.InputResponse, error) {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return port.InputResponse{}, nil
	}

	replyCh := make(chan port.InputResponse, 1)
	p.Send(bridgeMsg{
		ask: &bridgeAsk{
			request: req,
			replyCh: replyCh,
		},
	})

	select {
	case <-ctx.Done():
		return port.InputResponse{}, ctx.Err()
	case resp := <-replyCh:
		return resp, nil
	}
}
