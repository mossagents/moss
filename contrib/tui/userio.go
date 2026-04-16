package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/kernel/io"
	"sync"
)

// bridgeMsg 是从 UserIO 桥接到 Bubble Tea 的消息类型。
type bridgeMsg struct {
	output *io.OutputMessage
	ask    *bridgeAsk
}

type refreshMsg struct{}

// bridgeAsk 表示一个阻塞式用户输入请求。
type bridgeAsk struct {
	request io.InputRequest
	replyCh chan io.InputResponse
}

// bridgeIO 实现 io.UserIO，桥接 kernel 与 Bubble Tea TUI。
// kernel 在后台 goroutine 调用 Send/Ask，bridgeIO 将消息发送到 tea.Program。
type bridgeIO struct {
	program *tea.Program
	mu      sync.Mutex
}

// newBridgeIO 创建桥接器。需要在 tea.Program 创建后设置 program。
func newBridgeIO() *bridgeIO {
	return &bridgeIO{}
}

// SetProgram 设置 tea.Program 引用（需要在 Program 创建后立即调用）。
func (b *bridgeIO) SetProgram(p *tea.Program) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.program = p
}

// Send 向用户推送内容（非阻塞，通过 tea.Program.Send 注入消息）。
func (b *bridgeIO) Send(_ context.Context, msg io.OutputMessage) error {
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
func (b *bridgeIO) Refresh() {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return
	}
	p.Send(refreshMsg{})
}

func (b *bridgeIO) SendProgress(snapshot executionProgressState, setCurrent bool) {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return
	}
	p.Send(notificationProgressMsg{
		Snapshot:   snapshot,
		SetCurrent: setCurrent,
	})
}

// Ask 向用户请求输入（阻塞当前 goroutine，等待 TUI 回复）。
func (b *bridgeIO) Ask(ctx context.Context, req io.InputRequest) (io.InputResponse, error) {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return io.InputResponse{}, nil
	}

	replyCh := make(chan io.InputResponse, 1)
	p.Send(bridgeMsg{
		ask: &bridgeAsk{
			request: req,
			replyCh: replyCh,
		},
	})

	select {
	case <-ctx.Done():
		return io.InputResponse{}, ctx.Err()
	case resp := <-replyCh:
		return resp, nil
	}
}
