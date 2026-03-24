package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagi/moss/kernel/port"
)

// cliUserIO 是基于终端的 UserIO 实现（用于 run 命令的 CLI 模式）。
type cliUserIO struct {
	writer io.Writer
	reader *os.File
}

func (c *cliUserIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(c.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(c.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(c.writer)
	case port.OutputProgress:
		fmt.Fprintf(c.writer, "⏳ %s\n", msg.Content)
	case port.OutputToolStart:
		fmt.Fprintf(c.writer, "🔧 Running %s...\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			fmt.Fprintf(c.writer, "✅ %s\n", truncate(msg.Content, 200))
		}
	}
	return nil
}

func (c *cliUserIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	reader := bufio.NewReader(c.reader)
	switch req.Type {
	case port.InputConfirm:
		fmt.Fprintf(c.writer, "%s [y/N]: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		return port.InputResponse{Approved: line == "y" || line == "yes"}, nil

	case port.InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		var sel int
		fmt.Fscan(c.reader, &sel)
		return port.InputResponse{Selected: sel - 1}, nil

	default: // FreeText
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
