package io

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
	var err error
	switch msg.Type {
	case OutputText:
		_, err = fmt.Fprintln(c.W, msg.Content)
	case OutputStream:
		_, err = fmt.Fprint(c.W, msg.Content)
	case OutputStreamEnd:
		_, err = fmt.Fprintln(c.W)
	case OutputReasoning:
		_, err = fmt.Fprintf(c.W, "💭 %s\n", msg.Content)
	case OutputProgress:
		_, err = fmt.Fprintf(c.W, "⏳ %s\n", msg.Content)
	case OutputToolStart:
		_, err = fmt.Fprintf(c.W, "🔧 %s\n", msg.Content)
	case OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			_, err = fmt.Fprintf(c.W, "❌ %s\n", msg.Content)
		} else {
			content := msg.Content
			if ml := c.maxLen(); len(content) > ml {
				content = content[:ml] + "..."
			}
			_, err = fmt.Fprintf(c.W, "✅ %s\n", content)
		}
	}
	return err
}

func (c *ConsoleIO) Ask(_ context.Context, req InputRequest) (InputResponse, error) {
	reader := bufio.NewReader(c.R)

	switch req.Type {
	case InputConfirm:
		prompt := req.Prompt
		if req.Approval != nil && req.Approval.ToolName != "" {
			prompt = fmt.Sprintf("%s (tool=%s risk=%s)", req.Prompt, req.Approval.ToolName, req.Approval.Risk)
		}
		if _, err := fmt.Fprintf(c.W, "%s [y/N]: ", prompt); err != nil {
			return InputResponse{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		answer := strings.TrimSpace(strings.ToLower(line))
		approved := answer == "y" || answer == "yes"
		var decision *ApprovalDecision
		if req.Approval != nil {
			decision = &ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  approved,
				Source:    "console",
			}
		}
		return InputResponse{Approved: approved, Decision: decision}, nil

	case InputSelect:
		for i, opt := range req.Options {
			if _, err := fmt.Fprintf(c.W, "  %d) %s\n", i+1, opt); err != nil {
				return InputResponse{}, err
			}
		}
		if _, err := fmt.Fprintf(c.W, "%s: ", req.Prompt); err != nil {
			return InputResponse{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		var sel int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &sel); err != nil || sel < 1 || sel > len(req.Options) {
			return InputResponse{Selected: 0}, nil // 默认选第一项
		}
		return InputResponse{Selected: sel - 1}, nil

	case InputForm:
		form := make(map[string]any, len(req.Fields))
		for _, field := range req.Fields {
			title := field.Title
			if strings.TrimSpace(title) == "" {
				title = field.Name
			}
			switch field.Type {
			case InputFieldBoolean:
				if _, err := fmt.Fprintf(c.W, "%s [y/N]: ", title); err != nil {
					return InputResponse{}, err
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					return InputResponse{}, err
				}
				answer := strings.TrimSpace(strings.ToLower(line))
				form[field.Name] = answer == "y" || answer == "yes" || answer == "true"
			case InputFieldSingleSelect:
				for i, opt := range field.Options {
					if _, err := fmt.Fprintf(c.W, "  %d) %s\n", i+1, opt); err != nil {
						return InputResponse{}, err
					}
				}
				if _, err := fmt.Fprintf(c.W, "%s: ", title); err != nil {
					return InputResponse{}, err
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					return InputResponse{}, err
				}
				var sel int
				if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &sel); err != nil || sel < 1 || sel > len(field.Options) {
					if len(field.Options) > 0 {
						form[field.Name] = field.Options[0]
					}
				} else {
					form[field.Name] = field.Options[sel-1]
				}
			case InputFieldMultiSelect:
				for i, opt := range field.Options {
					if _, err := fmt.Fprintf(c.W, "  %d) %s\n", i+1, opt); err != nil {
						return InputResponse{}, err
					}
				}
				if _, err := fmt.Fprintf(c.W, "%s (comma-separated indexes): ", title); err != nil {
					return InputResponse{}, err
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					return InputResponse{}, err
				}
				line = strings.TrimSpace(line)
				chosen := []string{}
				if line != "" {
					parts := strings.Split(line, ",")
					for _, p := range parts {
						var idx int
						if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &idx); err == nil && idx >= 1 && idx <= len(field.Options) {
							chosen = append(chosen, field.Options[idx-1])
						}
					}
				}
				form[field.Name] = chosen
			default:
				if _, err := fmt.Fprintf(c.W, "%s: ", title); err != nil {
					return InputResponse{}, err
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					return InputResponse{}, err
				}
				form[field.Name] = strings.TrimSpace(line)
			}
		}
		return InputResponse{Form: form}, nil

	default:
		if _, err := fmt.Fprintf(c.W, "%s: ", req.Prompt); err != nil {
			return InputResponse{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return InputResponse{}, err
		}
		return InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

