package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagents/moss/kernel/port"
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
		prompt := req.Prompt
		if req.Approval != nil && req.Approval.ToolName != "" {
			prompt = fmt.Sprintf("%s (tool=%s risk=%s)", req.Prompt, req.Approval.ToolName, req.Approval.Risk)
		}
		fmt.Fprintf(c.writer, "%s [y/N]: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		approved := line == "y" || line == "yes"
		var decision *port.ApprovalDecision
		if req.Approval != nil {
			decision = &port.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  approved,
				Source:    "cli",
			}
		}
		return port.InputResponse{Approved: approved, Decision: decision}, nil

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
