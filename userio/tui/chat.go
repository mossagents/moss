package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

const (
	maxInputHistory = 500
)

// sessionResultMsg 表示 agent session 结束。
type sessionResultMsg struct {
	output string
	err    error
}

// cancelMsg 通知应用退出并清理资源。
type cancelMsg struct{}

// switchModelMsg 通知 app 切换模型。
type switchModelMsg struct {
	model string
}

// switchTrustMsg 通知 app 切换 trust level。
type switchTrustMsg struct {
	trust string
}

// chatModel 是对话主界面。
type chatModel struct {
	viewport  viewport.Model
	textarea  textarea.Model
	messages  []chatMessage
	streaming bool // 是否正在接收流式输出
	width     int
	height    int
	ready     bool

	// agent 交互
	sendFn              func(string)  // 发送用户消息给 agent
	cancelRunFn         func() bool   // 取消当前运行中的任务
	skillListFn         func() string // 查询已加载 skills
	sessionInfoFn       func() string
	offloadFn           func(keepRecent int, note string) (string, error)
	taskListFn          func(status string, limit int) (string, error)
	taskQueryFn         func(taskID string) (string, error)
	taskCancelFn        func(taskID, reason string) (string, error)
	sessionListFn       func(limit int) (string, error)
	sessionRestoreFn    func(sessionID string) (string, error)
	gitRunFn            func(cmd string, args []string) (string, error)
	permissionSummaryFn func() string
	setPermissionFn     func(toolName, mode string) (string, error)
	pendAsk             *bridgeAsk // 当前阻塞的 Ask 请求
	askForm             *askFormState
	finished            bool   // session 已结束
	result              string // 最终结果

	// 工具输出折叠
	toolCollapsed bool // true 时折叠 tool start/result 消息

	// 配置显示
	provider  string
	model     string
	workspace string
	trust     string

	queuedInputs []string

	inputHistory  []string
	historyCursor int
	historyDraft  string
	historyPath   string
	slashHints    []string

	now       func() time.Time
	lastEscAt time.Time
	lastCtrlC time.Time
}

func newChatModel(provider, model, workspace string) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send, Shift+Enter/Alt+Enter/Ctrl+J for newline)"
	ta.Focus()
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	// Bubble Tea does not reliably expose Shift+Enter on all terminals, so keep it
	// as a best-effort binding and add portable fallbacks for multiline input.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")

	return chatModel{
		textarea:      ta,
		provider:      provider,
		model:         model,
		workspace:     workspace,
		trust:         "trusted",
		toolCollapsed: true,
		inputHistory:  loadInputHistory(defaultHistoryPath(), maxInputHistory),
		historyPath:   defaultHistoryPath(),
		now:           time.Now,
	}
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		return m, nil

	case tea.KeyMsg:
		if m.pendAsk != nil && m.askForm != nil {
			switch msg.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "esc":
				return m.handleEsc()
			}
			return m.handleAskKey(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "esc":
			return m.handleEsc()
		case "ctrl+o":
			m.toolCollapsed = !m.toolCollapsed
			m.refreshViewport()
			return m, nil
		case "up", "down":
			if hints := m.currentSlashHints(); len(hints) > 0 {
				return m, nil
			}
			return m.handleHistoryNavigation(msg.String())
		case "tab":
			if m.applySlashCompletion() {
				m.adjustInputHeight()
				return m, nil
			}
			return m, nil
		case "enter":
			return m.handleSend()
		}

	case bridgeMsg:
		return m.handleBridge(msg)

	case refreshMsg:
		m.refreshViewport()
		return m, nil

	case sessionResultMsg:
		m.streaming = false
		m.finished = true
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.err.Error()})
		}
		if msg.output != "" {
			m.result = msg.output
		}
		if len(m.queuedInputs) > 0 && m.sendFn != nil {
			next := m.queuedInputs[0]
			m.queuedInputs = m.queuedInputs[1:]
			m.streaming = true
			m.finished = false
			m.sendFn(next)
		}
		m.refreshViewport()
		m.textarea.Focus()
		return m, nil
	}

	// 更新子组件
	if m.pendAsk == nil {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.adjustInputHeight()
		m.historyCursor = len(m.inputHistory)
		m.historyDraft = m.textarea.Value()
		m.refreshSlashHints()
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m chatModel) handleSend() (chatModel, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.recordInputHistory(text)
	m.historyCursor = len(m.inputHistory)
	m.historyDraft = ""

	// 斜杠命令
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// 如果有阻塞的 Ask 请求，回复它
	if m.pendAsk != nil {
		if m.askForm != nil {
			return m.submitAskForm()
		}
		ask := m.pendAsk
		m.pendAsk = nil
		m.messages = append(m.messages, chatMessage{kind: msgUser, content: text})
		m.textarea.Reset()
		m.adjustInputHeight()
		m.refreshViewport()

		// 构造回复
		resp := port.InputResponse{Value: text}
		if ask.request.Type == port.InputConfirm {
			text = strings.ToLower(text)
			resp.Approved = text == "y" || text == "yes"
		}
		ask.replyCh <- resp
		return m, nil
	}

	// 普通用户消息
	return m.dispatchUserSubmission(text, text)
}

func (m chatModel) handleBridge(msg bridgeMsg) (chatModel, tea.Cmd) {
	if msg.output != nil {
		o := msg.output
		switch o.Type {
		case port.OutputText:
			m.messages = append(m.messages, chatMessage{kind: msgAssistant, content: o.Content})
		case port.OutputStream:
			m.appendStream(o.Content)
		case port.OutputStreamEnd:
			m.streaming = false
		case port.OutputProgress:
			m.messages = append(m.messages, chatMessage{kind: msgProgress, content: o.Content})
		case port.OutputToolStart:
			m.messages = append(m.messages, chatMessage{kind: msgToolStart, content: o.Content})
		case port.OutputToolResult:
			isErr, _ := o.Meta["is_error"].(bool)
			if isErr {
				m.messages = append(m.messages, chatMessage{kind: msgToolError, content: o.Content})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgToolResult, content: o.Content})
			}
		}
		m.refreshViewport()
	}

	if msg.ask != nil {
		m.pendAsk = msg.ask
		m.askForm = newAskFormState(msg.ask.request)
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Interactive input requested. Use Tab to navigate and Enter to confirm."})
		m.activateAskField()
		m.refreshViewport()
	}

	return m, nil
}

func (m *chatModel) appendStream(delta string) {
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].kind == msgAssistant && m.streaming {
		m.messages[len(m.messages)-1].content += delta
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgAssistant, content: delta})
		m.streaming = true
	}
}

func (m *chatModel) refreshViewport() {
	content := renderAllMessages(m.messages, m.mainWidth(), m.toolCollapsed)
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m *chatModel) recalcLayout() {
	headerH := 2 // 顶栏
	metaH := 1
	inputH := m.inputBoxHeight()
	statusH := 1 // 底部状态栏

	vpHeight := m.height - headerH - metaH - inputH - statusH
	if vpHeight < 3 {
		vpHeight = 3
	}

	if !m.ready {
		m.viewport = viewport.New(m.mainWidth(), vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.mainWidth()
		m.viewport.Height = vpHeight
	}

	inputWidth := m.width - 4
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.textarea.SetWidth(inputWidth)
	m.adjustInputHeight()
	m.refreshViewport()
}

func (m *chatModel) inputBoxHeight() int {
	h := m.textarea.Height() + 2
	if h < 3 {
		return 3
	}
	if h > 7 {
		return 7
	}
	return h
}

func (m *chatModel) adjustInputHeight() {
	lines := wrappedLineCount(m.textarea.Value(), m.inputWrapWidth())
	if lines < 1 {
		lines = 1
	}
	if lines > 5 {
		lines = 5
	}
	m.textarea.SetHeight(lines)
}

func (m *chatModel) inputWrapWidth() int {
	width := m.width - 4
	if width < 1 {
		width = 1
	}
	return width
}

func wrappedLineCount(text string, width int) int {
	if width < 1 {
		width = 1
	}
	if text == "" {
		return 1
	}

	total := 0
	for _, line := range strings.Split(text, "\n") {
		lineWidth := runewidth.StringWidth(line)
		if lineWidth == 0 {
			total++
			continue
		}
		total += (lineWidth + width - 1) / width
	}
	return total
}

func (m chatModel) mainWidth() int {
	if m.width < 40 {
		return 40
	}
	return m.width
}

func (m chatModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var b strings.Builder

	// 顶栏
	header := titleStyle.Render("🌿 mosscode")
	info := statusBarStyle.Render(fmt.Sprintf("  %s │ %s", m.provider, m.workspace))
	if m.model != "" {
		info = statusBarStyle.Render(fmt.Sprintf("  %s (%s) │ %s", m.provider, m.model, m.workspace))
	}
	b.WriteString(topBarStyle.Render(header + info))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// 输入框上方元信息（左右）
	leftMeta := fmt.Sprintf("State: %s  Provider: %s", valueOrDefaultRunState(m.streaming), m.provider)
	if strings.TrimSpace(m.model) != "" {
		leftMeta = fmt.Sprintf("%s (%s)", leftMeta, m.model)
	}
	rightMeta := fmt.Sprintf("Trust: %s  Messages: %d", m.trust, len(m.messages))
	b.WriteString(lipgloss.JoinHorizontal(
		lipgloss.Top,
		mutedStyle.Width(m.mainWidth()/2).Render(leftMeta),
		mutedStyle.Width(m.mainWidth()-m.mainWidth()/2).Align(lipgloss.Right).Render(rightMeta),
	))
	b.WriteString("\n")

	// 消息区
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// 输入区
	if len(m.queuedInputs) > 0 {
		queueLines := make([]string, 0, len(m.queuedInputs)+1)
		queueLines = append(queueLines, fmt.Sprintf("Queued messages (%d)", len(m.queuedInputs)))
		for i, q := range m.queuedInputs {
			if i >= 5 {
				queueLines = append(queueLines, fmt.Sprintf("...and %d more", len(m.queuedInputs)-i))
				break
			}
			queueLines = append(queueLines, fmt.Sprintf("%d) %s", i+1, truncateForQueue(q, m.mainWidth()-12)))
		}
		b.WriteString(mutedStyle.Render("  " + strings.Join(queueLines, "  │  ")))
		b.WriteString("\n")
	}
	if m.pendAsk != nil && m.askForm != nil {
		b.WriteString(m.renderAskForm(m.mainWidth() - 2))
	} else {
		if m.streaming {
			b.WriteString(runningStyle.Render("  ● Running... (double Esc to cancel current run)"))
			b.WriteString("\n")
		}
		if hints := m.currentSlashHints(); len(hints) > 0 {
			b.WriteString(mutedStyle.Render("  Slash suggestions: " + strings.Join(hints, "  │  ") + "  (Tab to complete)"))
			b.WriteString("\n")
		}
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// 底部状态
	toolHint := "Ctrl+O collapse tools"
	if m.toolCollapsed {
		toolHint = "Ctrl+O expand tools"
	}
	leftStatus := fmt.Sprintf("/help commands │ %s", toolHint)
	rightStatus := "↑↓ history │ double Esc cancel run │ Ctrl+C clear (double quit)"
	status := lipgloss.JoinHorizontal(
		lipgloss.Top,
		mutedStyle.Width(m.mainWidth()/2).Render(leftStatus),
		mutedStyle.Width(m.mainWidth()-m.mainWidth()/2).Align(lipgloss.Right).Render(rightStatus),
	)
	if m.pendAsk != nil && m.askForm != nil {
		status = mutedStyle.Render("Tab/Shift+Tab move fields │ ↑↓ choose options │ Space toggle multi-select │ Enter confirm")
	} else if m.pendAsk != nil {
		status = mutedStyle.Render("Type your reply and press Enter │ double Esc cancel run │ Ctrl+C clear input")
	}
	b.WriteString(status)

	return b.String()
}

func (m chatModel) handleEsc() (chatModel, tea.Cmd) {
	now := m.now()
	if !m.lastEscAt.IsZero() && now.Sub(m.lastEscAt) <= 500*time.Millisecond {
		m.lastEscAt = time.Time{}
		if m.cancelRunFn != nil && m.cancelRunFn() {
			m.streaming = false
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Current run cancelled by user (Esc Esc)."})
			m.refreshViewport()
		}
		return m, nil
	}
	m.lastEscAt = now
	return m, nil
}

func (m chatModel) handleCtrlC() (chatModel, tea.Cmd) {
	now := m.now()
	if !m.lastCtrlC.IsZero() && now.Sub(m.lastCtrlC) <= 500*time.Millisecond {
		return m, func() tea.Msg { return cancelMsg{} }
	}
	m.lastCtrlC = now
	m.textarea.Reset()
	m.adjustInputHeight()
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Input cleared. Press Ctrl+C again quickly to exit."})
	m.refreshViewport()
	return m, nil
}

// handleSlashCommand 处理 / 开头的斜杠命令。
func (m chatModel) handleSlashCommand(input string) (chatModel, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	m.textarea.Reset()

	switch cmd {
	case "/exit", "/quit":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Goodbye 👋"})
		m.refreshViewport()
		return m, func() tea.Msg { return cancelMsg{} }

	case "/model":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("Current model: %s\nUsage: /model <model>", m.provider),
			})
			m.refreshViewport()
			return m, nil
		}
		newModel := strings.Join(args, " ")
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Switching model to %s...", newModel),
		})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg { return switchModelMsg{model: newModel} }

	case "/clear":
		m.messages = nil
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: "Conversation cleared.",
		})
		m.refreshViewport()
		return m, nil

	case "/skills":
		info := "Skill information is unavailable."
		if m.skillListFn != nil {
			info = m.skillListFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil

	case "/skill":
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /skill <name> <task...>"})
			m.refreshViewport()
			return m, nil
		}
		name := strings.TrimSpace(args[0])
		task := strings.TrimSpace(strings.Join(args[1:], " "))
		return m.invokeSkillLikeCommand(name, task, input)

	case "/session":
		if len(args) >= 2 && strings.ToLower(args[0]) == "restore" {
			if m.sessionRestoreFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Session restore is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			id := strings.TrimSpace(args[1])
			if id == "" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /session restore <session_id>"})
				m.refreshViewport()
				return m, nil
			}
			out, err := m.sessionRestoreFn(id)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to restore session: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		info := "Session information is unavailable."
		if m.sessionInfoFn != nil {
			info = m.sessionInfoFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil

	case "/sessions":
		if m.sessionListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Session list is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		limit := 20
		if len(args) >= 1 {
			v, err := strconv.Atoi(args[0])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /sessions [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.sessionListFn(limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list sessions: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/offload":
		if m.offloadFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "offload_context tool is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		keepRecent := 20
		note := "manual offload from TUI"
		if len(args) >= 1 {
			v, err := strconv.Atoi(args[0])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /offload [keep_recent:int] [note...]"})
				m.refreshViewport()
				return m, nil
			}
			keepRecent = v
		}
		if len(args) >= 2 {
			note = strings.Join(args[1:], " ")
		}
		out, err := m.offloadFn(keepRecent, note)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("offload failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/tasks":
		if m.taskListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background task tool is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		status := ""
		limit := 20
		if len(args) >= 1 {
			status = strings.TrimSpace(strings.ToLower(args[0]))
			switch status {
			case "", "running", "completed", "failed", "cancelled":
			default:
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /tasks [running|completed|failed|cancelled] [limit]"})
				m.refreshViewport()
				return m, nil
			}
		}
		if len(args) >= 2 {
			v, err := strconv.Atoi(args[1])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /tasks [status] [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.taskListFn(status, limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list tasks: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/task":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /task <task_id>\n  /task cancel <task_id> [reason...]"})
			m.refreshViewport()
			return m, nil
		}
		if args[0] == "cancel" {
			if m.taskCancelFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Task cancellation tool is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /task cancel <task_id> [reason...]"})
				m.refreshViewport()
				return m, nil
			}
			taskID := strings.TrimSpace(args[1])
			reason := "cancelled by user from TUI"
			if len(args) >= 3 {
				reason = strings.Join(args[2:], " ")
			}
			out, err := m.taskCancelFn(taskID, reason)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to cancel task: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		if m.taskQueryFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Task query tool is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		taskID := strings.TrimSpace(args[0])
		out, err := m.taskQueryFn(taskID)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to query task: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/config":
		return m.handleConfigCommand(args)

	case "/git":
		if m.gitRunFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Git workflow commands are unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /git status\n  /git diff [path]\n  /git commit <message>\n  /git pr [args...]"})
			m.refreshViewport()
			return m, nil
		}
		sub := strings.ToLower(args[0])
		switch sub {
		case "status":
			out, err := m.gitRunFn("git", []string{"--no-pager", "status", "--short"})
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git status failed: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
		case "diff":
			cmdArgs := []string{"--no-pager", "diff"}
			if len(args) > 1 {
				cmdArgs = append(cmdArgs, args[1:]...)
			}
			out, err := m.gitRunFn("git", cmdArgs)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git diff failed: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
		case "commit":
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /git commit <message>"})
			} else {
				msg := strings.Join(args[1:], " ")
				out, err := m.gitRunFn("git", []string{"commit", "-m", msg})
				if err != nil {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git commit failed: %v", err)})
				} else {
					m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
				}
			}
		case "pr":
			prArgs := []string{"pr"}
			if len(args) > 1 {
				prArgs = append(prArgs, args[1:]...)
			} else {
				prArgs = append(prArgs, "status")
			}
			out, err := m.gitRunFn("gh", prArgs)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("gh pr failed: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /git status | /git diff [path] | /git commit <message> | /git pr [args...]"})
		}
		m.refreshViewport()
		return m, nil

	case "/budget":
		info := "Budget information is unavailable."
		if m.sessionInfoFn != nil {
			info = m.sessionInfoFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil

	case "/permissions":
		if len(args) == 0 {
			info := "Permission policy information is unavailable."
			if m.permissionSummaryFn != nil {
				info = m.permissionSummaryFn()
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
			m.refreshViewport()
			return m, nil
		}
		if strings.ToLower(args[0]) == "trust" {
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions trust <trusted|restricted>"})
				m.refreshViewport()
				return m, nil
			}
			nextTrust := strings.ToLower(strings.TrimSpace(args[1]))
			if nextTrust != "trusted" && nextTrust != "restricted" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "trust must be trusted or restricted"})
				m.refreshViewport()
				return m, nil
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching trust to %s...", nextTrust)})
			m.streaming = true
			m.refreshViewport()
			return m, func() tea.Msg { return switchTrustMsg{trust: nextTrust} }
		}
		if strings.ToLower(args[0]) == "set" {
			if len(args) < 3 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions set <tool_name> <allow|ask|deny|reset>"})
				m.refreshViewport()
				return m, nil
			}
			if m.setPermissionFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Runtime permissions are unavailable."})
				m.refreshViewport()
				return m, nil
			}
			toolName := strings.TrimSpace(args[1])
			mode := strings.ToLower(strings.TrimSpace(args[2]))
			out, err := m.setPermissionFn(toolName, mode)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to set permission: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions\n  /permissions trust <trusted|restricted>\n  /permissions set <tool_name> <allow|ask|deny|reset>"})
		m.refreshViewport()
		return m, nil

	case "/trust":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current trust: %s\nUsage: /trust <trusted|restricted>", m.trust)})
			m.refreshViewport()
			return m, nil
		}
		nextTrust := strings.ToLower(strings.TrimSpace(args[0]))
		if nextTrust != "trusted" && nextTrust != "restricted" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "trust must be trusted or restricted"})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching trust to %s...", nextTrust)})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg { return switchTrustMsg{trust: nextTrust} }

	case "/help":
		help := "Available commands:\n" +
			"  /model [name]  Show or switch model\n" +
			"  /config        Show current config\n" +
			"  /config set <key> <value>  Set config key (provider/model/base_url/api_key)\n" +
			"  /skills        Show discovered user skills and activation state\n" +
			"  /skill <name> <task...>  Invoke a specific skill/tool by name\n" +
			"  /<name> <task...>  Shortcut for /skill <name> <task...>\n" +
			"  /session       Show current session summary\n" +
			"  /session restore <id>  Restore a persisted session\n" +
			"  /sessions [limit]  List persisted sessions\n" +
			"  /offload [keep_recent] [note]  Compact context and persist snapshot\n" +
			"  /tasks [status] [limit]  List background tasks\n" +
			"  /task <id>     Query task details\n" +
			"  /task cancel <id> [reason]  Cancel a background task\n" +
			"  /git status|diff|commit|pr  Common git workflow helpers\n" +
			"  /budget        Show budget/context summary\n" +
			"  /permissions   Show runtime permission summary\n" +
			"  /permissions set <tool> <allow|ask|deny|reset>\n" +
			"  /permissions trust <trusted|restricted>\n" +
			"  /trust <trusted|restricted>  Switch trust and rebuild runtime\n" +
			"  /clear         Clear conversation\n" +
			"\nKeyboard shortcuts:\n" +
			"  double Esc     Cancel current running generation/tool execution\n" +
			"  Ctrl+C         Clear input (press twice quickly to quit)\n" +
			"  Ctrl+O         Collapse/expand tool messages\n" +
			"  Enter (running) Queue message to run after current task\n" +
			"  Up/Down        Navigate persisted input history\n" +
			"  Shift+Enter    Insert newline\n" +
			"  /help          Show this help\n" +
			"  /exit          Exit mosscode"
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: help})
		m.refreshViewport()
		return m, nil

	default:
		if strings.HasPrefix(cmd, "/") && len(cmd) > 1 {
			name := strings.TrimSpace(strings.TrimPrefix(cmd, "/"))
			task := strings.TrimSpace(strings.Join(args, " "))
			if task == "" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /<skill_or_tool_name> <task...>"})
				m.refreshViewport()
				return m, nil
			}
			return m.invokeSkillLikeCommand(name, task, input)
		}
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Unknown command: %s (use /help to list commands)", cmd),
		})
		m.refreshViewport()
		return m, nil
	}
}

// handleConfigCommand 处理 /config 命令。
func (m chatModel) handleConfigCommand(args []string) (chatModel, tea.Cmd) {
	cfgPath := appconfig.DefaultGlobalConfigPath()
	if cfgPath == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Unable to determine config directory."})
		m.refreshViewport()
		return m, nil
	}

	// /config — 显示当前配置
	if len(args) == 0 {
		cfg, _ := appconfig.LoadConfig(cfgPath)
		apiKeyDisplay := "(not set)"
		if cfg.APIKey != "" {
			apiKeyDisplay = maskKey(cfg.APIKey)
		}
		info := fmt.Sprintf("Config file: `%s`\n\n  provider: %s\n  model:    %s\n  base_url: %s\n  api_key:  %s",
			cfgPath,
			valueOrDefault(cfg.Provider, "(not set)"),
			valueOrDefault(cfg.Model, "(not set)"),
			valueOrDefault(cfg.BaseURL, "(not set)"),
			apiKeyDisplay,
		)
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil
	}

	// /config set <key> <value>
	if args[0] == "set" && len(args) >= 3 {
		key := strings.ToLower(args[1])
		value := strings.Join(args[2:], " ")

		cfg, _ := appconfig.LoadConfig(cfgPath)
		switch key {
		case "provider":
			cfg.Provider = value
		case "model":
			cfg.Model = value
		case "base_url", "baseurl":
			cfg.BaseURL = value
		case "api_key", "apikey":
			cfg.APIKey = value
		default:
			m.messages = append(m.messages, chatMessage{
				kind:    msgError,
				content: fmt.Sprintf("Unknown config key: %s (supported: provider, model, base_url, api_key)", key),
			})
			m.refreshViewport()
			return m, nil
		}

		if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
			m.messages = append(m.messages, chatMessage{
				kind:    msgError,
				content: fmt.Sprintf("Failed to save config: %v", err),
			})
		} else {
			display := value
			if key == "api_key" || key == "apikey" {
				display = maskKey(value)
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("Set %s = %s\nNote: some settings require restarting moss or switching via /model.", key, display),
			})
		}
		m.refreshViewport()
		return m, nil
	}

	m.messages = append(m.messages, chatMessage{
		kind:    msgSystem,
		content: "Usage:\n  /config              Show current config\n  /config set <key> <value>  Set config key",
	})
	m.refreshViewport()
	return m, nil
}

// maskKey 遮盖 API key 只显示前4和后4位。
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func valueOrDefaultRunState(running bool) string {
	if running {
		return "running"
	}
	return "idle"
}

func (m chatModel) handleHistoryNavigation(direction string) (chatModel, tea.Cmd) {
	if len(m.inputHistory) == 0 {
		return m, nil
	}
	if direction == "up" {
		if m.historyCursor >= len(m.inputHistory) {
			m.historyDraft = m.textarea.Value()
			m.historyCursor = len(m.inputHistory)
		}
		if m.historyCursor > 0 {
			m.historyCursor--
			m.textarea.SetValue(m.inputHistory[m.historyCursor])
			m.adjustInputHeight()
		}
		return m, nil
	}
	if direction == "down" {
		if m.historyCursor < len(m.inputHistory) {
			m.historyCursor++
			if m.historyCursor == len(m.inputHistory) {
				m.textarea.SetValue(m.historyDraft)
			} else {
				m.textarea.SetValue(m.inputHistory[m.historyCursor])
			}
			m.adjustInputHeight()
		}
		return m, nil
	}
	return m, nil
}

func (m *chatModel) recordInputHistory(input string) {
	input = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", " "), "\n", " "))
	if input == "" {
		return
	}
	filtered := make([]string, 0, len(m.inputHistory)+1)
	for _, item := range m.inputHistory {
		if item != input {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, input)
	if len(filtered) > maxInputHistory {
		filtered = filtered[len(filtered)-maxInputHistory:]
	}
	m.inputHistory = filtered
	m.historyCursor = len(m.inputHistory)
	_ = saveInputHistory(m.historyPath, m.inputHistory)
}

func defaultHistoryPath() string {
	appDir := appconfig.AppDir()
	if appDir == "" {
		return ""
	}
	return filepath.Join(appDir, "input_history")
}

func loadInputHistory(path string, max int) []string {
	if path == "" || max <= 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		v := strings.TrimSpace(line)
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func saveInputHistory(path string, history []string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload := strings.Join(history, "\n")
	if payload != "" {
		payload += "\n"
	}
	return os.WriteFile(path, []byte(payload), 0o600)
}

func truncateForQueue(s string, max int) string {
	if max < 12 {
		max = 12
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (m *chatModel) refreshSlashHints() {
	text := strings.TrimSpace(m.textarea.Value())
	m.slashHints = filterSlashHints(text)
}

func (m chatModel) currentSlashHints() []string {
	if m.streaming || m.pendAsk != nil {
		return nil
	}
	return m.slashHints
}

func (m *chatModel) applySlashCompletion() bool {
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		return false
	}
	current := strings.TrimSpace(m.textarea.Value())
	if strings.Contains(current, " ") {
		return false
	}
	m.textarea.SetValue(hints[0] + " ")
	m.refreshSlashHints()
	return true
}

var slashCandidates = []string{
	"/help", "/skills", "/skill", "/session", "/sessions", "/offload", "/tasks", "/task",
	"/config", "/git", "/budget", "/permissions", "/trust", "/model", "/clear", "/exit", "/quit",
	"/http_request",
}

func filterSlashHints(input string) []string {
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	if strings.Contains(input, " ") {
		return nil
	}
	lower := strings.ToLower(input)
	hints := make([]string, 0, 8)
	for _, c := range slashCandidates {
		if strings.HasPrefix(c, lower) {
			hints = append(hints, c)
		}
	}
	if len(hints) == 0 {
		return nil
	}
	if len(hints) > 8 {
		hints = hints[:8]
	}
	return hints
}

func (m chatModel) invokeSkillLikeCommand(name, task, displayText string) (chatModel, tea.Cmd) {
	if name == "" || task == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /skill <name> <task...>"})
		m.refreshViewport()
		return m, nil
	}
	prompt := fmt.Sprintf("Use skill or tool '%s' to complete this request:\n%s", name, task)
	return m.dispatchUserSubmission(displayText, prompt)
}

func (m chatModel) dispatchUserSubmission(displayText, runText string) (chatModel, tea.Cmd) {
	if m.streaming {
		m.queuedInputs = append(m.queuedInputs, runText)
		m.textarea.Reset()
		m.adjustInputHeight()
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgUser, content: strings.TrimSpace(displayText)})
	m.textarea.Reset()
	m.adjustInputHeight()
	m.adjustInputHeight()
	m.streaming = true
	m.refreshViewport()
	if m.sendFn != nil {
		m.sendFn(runText)
	}
	return m, nil
}

// valueOrDefault 返回 s 或 defaultVal。
func valueOrDefault(s, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}
