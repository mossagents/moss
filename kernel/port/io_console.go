package port

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// ConsoleIO 是面向终端的 UserIO 实现，支持读写交互。
//
// 输出带有 emoji 前缀的格式化消息，Ask 通过标准输入读取用户回复。
// 适用于 REPL 类应用（mossclaw、mossquant 等）。
type ConsoleIO struct {
	W io.Writer
	R io.Reader

	// MaxResultLen 控制工具结果输出的最大字符数，超出截断。
	// 默认 300。
	MaxResultLen int
}

var _ UserIO = (*ConsoleIO)(nil)

// NewConsoleIO 创建面向标准输入输出的 ConsoleIO。
func NewConsoleIO() *ConsoleIO {
	return &ConsoleIO{W: defaultStdout(), R: defaultStdin(), MaxResultLen: 300}
}

func (c *ConsoleIO) maxLen() int {
	if c.MaxResultLen > 0 {
		return c.MaxResultLen
	}
	return 300
}

func (c *ConsoleIO) Send(_ context.Context, msg OutputMessage) error {
	switch msg.Type {
	case OutputText:
		fmt.Fprintln(c.W, msg.Content)
	case OutputStream:
		fmt.Fprint(c.W, msg.Content)
	case OutputStreamEnd:
		fmt.Fprintln(c.W)
	case OutputProgress:
		fmt.Fprintf(c.W, "⏳ %s\n", msg.Content)
	case OutputToolStart:
		fmt.Fprintf(c.W, "🔧 %s\n", msg.Content)
	case OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.W, "❌ %s\n", msg.Content)
		} else {
			content := msg.Content
			if ml := c.maxLen(); len(content) > ml {
				content = content[:ml] + "..."
			}
			fmt.Fprintf(c.W, "✅ %s\n", content)
		}
	}
	return nil
}

func (c *ConsoleIO) Ask(_ context.Context, req InputRequest) (InputResponse, error) {
	reader := bufio.NewReader(c.R)

	switch req.Type {
	case InputConfirm:
		fmt.Fprintf(c.W, "%s [y/N]: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		answer := strings.TrimSpace(strings.ToLower(line))
		return InputResponse{Approved: answer == "y" || answer == "yes"}, nil

	case InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.W, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.W, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		var sel int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &sel); err != nil || sel < 1 || sel > len(req.Options) {
			return InputResponse{Selected: 0}, nil // 默认选第一项
		}
		return InputResponse{Selected: sel - 1}, nil

	default:
		fmt.Fprintf(c.W, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		return InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}
