package tui

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// bangResultMsg 携带 shell 命令的执行结果。
type bangResultMsg struct {
	output string
	err    error
}

// handleBangCommand 处理 !<command> 语法，在工作目录中异步执行 shell 命令。
func (m chatModel) handleBangCommand(input string) (chatModel, tea.Cmd) {
	cmdLine := strings.TrimSpace(strings.TrimPrefix(input, "!"))
	if cmdLine == "" {
		return m, nil
	}

	m.textarea.Reset()
	m.adjustInputHeight()

	// 在聊天区域展示命令（与普通用户消息一致的气泡样式，前缀 $ 区分 shell 命令）
	m.messages = append(m.messages, chatMessage{
		kind:    msgUser,
		content: "$ " + cmdLine,
		meta:    map[string]any{"timestamp": m.now().UTC()},
	})
	m.streaming = true
	m.activeRunSummary = summarizeActiveRun(cmdLine)
	m.runStartedAt = m.now().UTC()
	m.pinnedToBottom = true
	m.refreshViewport()

	workspace := m.workspace
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.cancelRunFn = func() bool {
		cancel()
		return true
	}

	return m, func() tea.Msg {
		defer cancel()

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", cmdLine)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
		}
		if workspace != "" {
			cmd.Dir = workspace
		}

		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		err := cmd.Run()
		output := strings.TrimRight(buf.String(), "\r\n")
		return bangResultMsg{output: output, err: err}
	}
}
