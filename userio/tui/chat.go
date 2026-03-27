package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
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
	sendFn        func(string)  // 发送用户消息给 agent
	cancelRunFn   func() bool   // 取消当前运行中的任务
	skillListFn   func() string // 查询已加载 skills
	sessionInfoFn func() string
	offloadFn     func(keepRecent int, note string) (string, error)
	taskListFn    func(status string, limit int) (string, error)
	taskQueryFn   func(taskID string) (string, error)
	taskCancelFn  func(taskID, reason string) (string, error)
	pendAsk       *bridgeAsk // 当前阻塞的 Ask 请求
	askForm       *askFormState
	finished      bool       // session 已结束
	result        string     // 最终结果

	// 工具输出折叠
	toolCollapsed bool // true 时折叠 tool start/result 消息

	// 配置显示
	provider  string
	model     string
	workspace string

	sidebarTitle  string
	renderSidebar func() string

	now       func() time.Time
	lastEscAt time.Time
	lastCtrlC time.Time
}

func newChatModel(provider, model, workspace string) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send, /help for commands)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")

	return chatModel{
		textarea:  ta,
		provider:  provider,
		model:     model,
		workspace: workspace,
		now:       time.Now,
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
		case "ctrl+t":
			m.toolCollapsed = !m.toolCollapsed
			m.refreshViewport()
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
		m.refreshViewport()
		m.textarea.Focus()
		return m, nil
	}

	// 更新子组件
	if m.pendAsk == nil && !m.streaming {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
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
	m.messages = append(m.messages, chatMessage{kind: msgUser, content: text})
	m.textarea.Reset()
	m.streaming = true
	m.refreshViewport()

	if m.sendFn != nil {
		m.sendFn(text)
	}
	return m, nil
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
	inputH := 5  // 输入区（含边框）
	statusH := 1 // 底部状态栏

	vpHeight := m.height - headerH - inputH - statusH
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

	m.textarea.SetWidth(m.width - 4)
	m.refreshViewport()
}

func (m chatModel) sidebarVisible() bool {
	return m.width >= 100
}

func (m chatModel) sidebarWidth() int {
	if !m.sidebarVisible() {
		return 0
	}
	width := m.width / 3
	if width < 32 {
		width = 32
	}
	if width > 46 {
		width = 46
	}
	return width
}

func (m chatModel) mainWidth() int {
	if !m.sidebarVisible() {
		return m.width
	}
	mainWidth := m.width - m.sidebarWidth() - 1
	if mainWidth < 40 {
		return 40
	}
	return mainWidth
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

	// 消息区
	body := m.viewport.View()
	if m.sidebarVisible() {
		title := strings.TrimSpace(m.sidebarTitle)
		if title == "" {
			title = "Control Center"
		}
		sidebarContent := m.renderBuiltinSidebar()
		if m.renderSidebar != nil {
			external := strings.TrimSpace(m.renderSidebar())
			if external != "" {
				sidebarContent += "\n\n" + sidebarSectionTitleStyle.Render("Workspace") + "\n" + external
			}
		}
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(m.mainWidth()).Render(body),
			sidebarBoxStyle.Width(m.sidebarWidth()).Render(
				sidebarTitleStyle.Render(title)+"\n\n"+sidebarContent,
			),
		)
	}
	b.WriteString(body)
	b.WriteString("\n")

	// 输入区
	if m.pendAsk != nil && m.askForm != nil {
		b.WriteString(m.renderAskForm(m.mainWidth() - 2))
	} else if m.streaming {
		b.WriteString(runningStyle.Render("  ● Running...  (Esc Esc to cancel current run)"))
	} else {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// 底部状态
	toolHint := "Ctrl+T collapse tools"
	if m.toolCollapsed {
		toolHint = "Ctrl+T expand tools"
	}
	status := mutedStyle.Render(fmt.Sprintf("/help commands │ %s │ Esc Esc cancel run │ Ctrl+C clear input (double Ctrl+C quit)", toolHint))
	if m.pendAsk != nil && m.askForm != nil {
		status = mutedStyle.Render("Tab/Shift+Tab move fields │ ↑↓ choose options │ Space toggle multi-select │ Enter confirm")
	} else if m.pendAsk != nil {
		status = mutedStyle.Render("Type your reply and press Enter │ Esc Esc cancel run │ Ctrl+C clear input")
	}
	b.WriteString(status)

	return b.String()
}

func (m chatModel) renderBuiltinSidebar() string {
	runState := "idle"
	if m.streaming {
		runState = "running"
	}
	toolCount := 0
	progressCount := 0
	lastTool := "none"
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.kind == msgToolStart || msg.kind == msgToolResult || msg.kind == msgToolError {
			toolCount++
			if lastTool == "none" {
				lastTool = strings.TrimSpace(msg.content)
				if len(lastTool) > 50 {
					lastTool = lastTool[:50] + "..."
				}
			}
		}
		if msg.kind == msgProgress {
			progressCount++
		}
	}
	var sb strings.Builder
	sb.WriteString(sidebarSectionTitleStyle.Render("Session"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("State: %s\n", runState))
	sb.WriteString(fmt.Sprintf("Provider: %s\n", m.provider))
	if strings.TrimSpace(m.model) != "" {
		sb.WriteString(fmt.Sprintf("Model: %s\n", m.model))
	}
	sb.WriteString(fmt.Sprintf("Messages: %d\n", len(m.messages)))
	sb.WriteString("\n")
	sb.WriteString(sidebarSectionTitleStyle.Render("Tools"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Events: %d\n", toolCount))
	sb.WriteString(fmt.Sprintf("Progress: %d\n", progressCount))
	sb.WriteString(fmt.Sprintf("Last: %s\n", lastTool))
	sb.WriteString("\n")
	sb.WriteString(sidebarSectionTitleStyle.Render("Keys"))
	sb.WriteString("\n")
	sb.WriteString("Esc Esc  cancel current run\n")
	sb.WriteString("Ctrl+T   toggle tool fold\n")
	sb.WriteString("Ctrl+C   clear input\n")
	sb.WriteString("Ctrl+C×2 quit")
	return sb.String()
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

	case "/session":
		info := "Session information is unavailable."
		if m.sessionInfoFn != nil {
			info = m.sessionInfoFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
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

	case "/help":
		help := "Available commands:\n" +
			"  /model [name]  Show or switch model\n" +
			"  /config        Show current config\n" +
			"  /config set <key> <value>  Set config key (provider/model/base_url/api_key)\n" +
			"  /skills        Show discovered user skills and activation state\n" +
			"  /session       Show current session summary\n" +
			"  /offload [keep_recent] [note]  Compact context and persist snapshot\n" +
			"  /tasks [status] [limit]  List background tasks\n" +
			"  /task <id>     Query task details\n" +
			"  /task cancel <id> [reason]  Cancel a background task\n" +
			"  /clear         Clear conversation\n" +
			"\nKeyboard shortcuts:\n" +
			"  Esc Esc        Cancel current running generation/tool execution\n" +
			"  Ctrl+C         Clear input (press twice quickly to quit)\n" +
			"  Ctrl+T         Collapse/expand tool messages\n" +
			"  Shift+Enter    Insert newline\n" +
			"  /help          Show this help\n" +
			"  /exit          Exit mosscode"
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: help})
		m.refreshViewport()
		return m, nil

	default:
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
		info := fmt.Sprintf("Config file: %s\n\n  provider: %s\n  model:    %s\n  base_url: %s\n  api_key:  %s",
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

// valueOrDefault 返回 s 或 defaultVal。
func valueOrDefault(s, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}
