package channel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagi/moss/kernel/port"
)

// CLI 是基于 stdin/stdout 的终端 Channel 实现。
type CLI struct {
	prompt string
	in     io.Reader
	out    io.Writer
}

// CLIOption 配置 CLI Channel。
type CLIOption func(*CLI)

// WithPrompt 设置输入提示符。
func WithPrompt(p string) CLIOption {
	return func(c *CLI) { c.prompt = p }
}

// WithReader 设置输入源（默认 os.Stdin）。
func WithReader(r io.Reader) CLIOption {
	return func(c *CLI) { c.in = r }
}

// WithWriter 设置输出目的（默认 os.Stdout）。
func WithWriter(w io.Writer) CLIOption {
	return func(c *CLI) { c.out = w }
}

// NewCLI 创建终端 Channel。
func NewCLI(opts ...CLIOption) *CLI {
	c := &CLI{
		prompt: "> ",
		in:     os.Stdin,
		out:    os.Stdout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *CLI) Name() string { return "cli" }

// Receive 返回用户输入消息流。
// 读取 stdin 的每一行作为一条消息。遇到 EOF 或 ctx 取消时关闭 channel。
// /exit 命令会关闭流。
func (c *CLI) Receive(ctx context.Context) <-chan port.InboundMessage {
	ch := make(chan port.InboundMessage)

	go func() {
		defer close(ch)
		reader := bufio.NewReader(c.in)

		for {
			fmt.Fprint(c.out, c.prompt)
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}

			input := strings.TrimSpace(line)
			if input == "" {
				continue
			}

			// 内置退出命令
			lower := strings.ToLower(input)
			if lower == "/exit" || lower == "/quit" {
				fmt.Fprintln(c.out, "Bye!")
				return
			}

			msg := port.InboundMessage{
				ChannelName: "cli",
				SenderID:    "cli",
				Content:     input,
			}

			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

// Send 将回复输出到终端。
func (c *CLI) Send(_ context.Context, msg port.OutboundMessage) error {
	_, err := fmt.Fprintln(c.out, msg.Content)
	return err
}

// Close 是无操作（stdin/stdout 不应被关闭）。
func (c *CLI) Close() error { return nil }

// 确保 CLI 实现了 Channel 接口。
var _ port.Channel = (*CLI)(nil)
