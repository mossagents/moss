package tui

import (
	"fmt"
	"strconv"
	"strings"

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
	skillListFn   func() string // 查询已加载 skills
	sessionInfoFn func() string
	offloadFn     func(keepRecent int, note string) (string, error)
	taskListFn    func(status string, limit int) (string, error)
	taskQueryFn   func(taskID string) (string, error)
	taskCancelFn  func(taskID, reason string) (string, error)
	pendAsk       *bridgeAsk // 当前阻塞的 Ask 请求
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
}

func newChatModel(provider, model, workspace string) chatModel {
	ta := textarea.New()
	ta.Placeholder = "输入消息... (Enter 发送, /help 查看命令)"
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
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return cancelMsg{} }
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
		// 显示提问
		prompt := msg.ask.request.Prompt
		if len(msg.ask.request.Options) > 0 {
			for i, opt := range msg.ask.request.Options {
				prompt += fmt.Sprintf("\n  %d) %s", i+1, opt)
			}
		}
		m.messages = append(m.messages, chatMessage{kind: msgAssistant, content: prompt})
		m.refreshViewport()
		m.textarea.Focus()
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
	return m.renderSidebar != nil && m.width >= 100
}

func (m chatModel) sidebarWidth() int {
	if !m.sidebarVisible() {
		return 0
	}
	width := m.width / 3
	if width < 32 {
		width = 32
	}
	if width > 42 {
		width = 42
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
		return "加载中..."
	}

	var b strings.Builder

	// 顶栏
	header := titleStyle.Render("🌿 moss")
	info := statusBarStyle.Render(fmt.Sprintf("  %s │ %s", m.provider, m.workspace))
	if m.model != "" {
		info = statusBarStyle.Render(fmt.Sprintf("  %s (%s) │ %s", m.provider, m.model, m.workspace))
	}
	b.WriteString(header + info + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// 消息区
	body := m.viewport.View()
	if m.sidebarVisible() {
		sidebarContent := "暂无摘要。"
		if m.renderSidebar != nil {
			sidebarContent = m.renderSidebar()
		}
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(m.mainWidth()).Render(body),
			sidebarBoxStyle.Width(m.sidebarWidth()).Render(
				sidebarTitleStyle.Render(m.sidebarTitle)+"\n\n"+sidebarContent,
			),
		)
	}
	b.WriteString(body)
	b.WriteString("\n")

	// 输入区
	if m.streaming {
		b.WriteString(mutedStyle.Render("  ● 思考中..."))
	} else {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// 底部状态
	toolHint := "Ctrl+T 折叠工具"
	if m.toolCollapsed {
		toolHint = "Ctrl+T 展开工具"
	}
	status := mutedStyle.Render(fmt.Sprintf("/help 查看命令 │ %s │ Ctrl+C 退出", toolHint))
	if m.pendAsk != nil {
		status = mutedStyle.Render("输入回复后按 Enter 发送 │ /help 查看命令")
	}
	b.WriteString(status)

	return b.String()
}

// handleSlashCommand 处理 / 开头的斜杠命令。
func (m chatModel) handleSlashCommand(input string) (chatModel, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	m.textarea.Reset()

	switch cmd {
	case "/exit", "/quit":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "再见 👋"})
		m.refreshViewport()
		return m, func() tea.Msg { return cancelMsg{} }

	case "/model":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("当前模型: %s\n用法: /model <模型名称>", m.provider),
			})
			m.refreshViewport()
			return m, nil
		}
		newModel := strings.Join(args, " ")
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("正在切换到模型 %s...", newModel),
		})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg { return switchModelMsg{model: newModel} }

	case "/clear":
		m.messages = nil
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: "对话已清空。",
		})
		m.refreshViewport()
		return m, nil

	case "/skills":
		info := "skill 信息不可用。"
		if m.skillListFn != nil {
			info = m.skillListFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil

	case "/session":
		info := "session 信息不可用。"
		if m.sessionInfoFn != nil {
			info = m.sessionInfoFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil

	case "/offload":
		if m.offloadFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "offload_context 工具不可用。"})
			m.refreshViewport()
			return m, nil
		}
		keepRecent := 20
		note := "manual offload from TUI"
		if len(args) >= 1 {
			v, err := strconv.Atoi(args[0])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "用法: /offload [keep_recent:int] [note...]"})
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
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("offload 失败: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/tasks":
		if m.taskListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "后台任务工具不可用。"})
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
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "用法: /tasks [running|completed|failed|cancelled] [limit]"})
				m.refreshViewport()
				return m, nil
			}
		}
		if len(args) >= 2 {
			v, err := strconv.Atoi(args[1])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "用法: /tasks [status] [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.taskListFn(status, limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("获取任务列表失败: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/task":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "用法:\n  /task <task_id>\n  /task cancel <task_id> [reason...]"})
			m.refreshViewport()
			return m, nil
		}
		if args[0] == "cancel" {
			if m.taskCancelFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "取消任务工具不可用。"})
				m.refreshViewport()
				return m, nil
			}
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "用法: /task cancel <task_id> [reason...]"})
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
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("取消任务失败: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		if m.taskQueryFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "查询任务工具不可用。"})
			m.refreshViewport()
			return m, nil
		}
		taskID := strings.TrimSpace(args[0])
		out, err := m.taskQueryFn(taskID)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("查询任务失败: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/config":
		return m.handleConfigCommand(args)

	case "/help":
		help := "可用命令:\n" +
			"  /model [名称]  查看或切换模型\n" +
			"  /config        查看当前配置\n" +
			"  /config set <key> <value>  设置配置项 (provider/model/base_url/api_key)\n" +
			"  /skills        查看已加载的 skills\n" +
			"  /session       查看当前 session 概览\n" +
			"  /offload [keep_recent] [note]  压缩上下文并持久化快照\n" +
			"  /tasks [status] [limit]  列出后台任务\n" +
			"  /task <id>     查询后台任务详情\n" +
			"  /task cancel <id> [reason]  取消后台任务\n" +
			"  /clear         清空对话记录\n" +
			"  /help          显示此帮助\n" +
			"  /exit          退出 moss"
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: help})
		m.refreshViewport()
		return m, nil

	default:
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("未知命令: %s (输入 /help 查看可用命令)", cmd),
		})
		m.refreshViewport()
		return m, nil
	}
}

// handleConfigCommand 处理 /config 命令。
func (m chatModel) handleConfigCommand(args []string) (chatModel, tea.Cmd) {
	cfgPath := appconfig.DefaultGlobalConfigPath()
	if cfgPath == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "无法确定配置目录。"})
		m.refreshViewport()
		return m, nil
	}

	// /config — 显示当前配置
	if len(args) == 0 {
		cfg, _ := appconfig.LoadConfig(cfgPath)
		apiKeyDisplay := "(未设置)"
		if cfg.APIKey != "" {
			apiKeyDisplay = maskKey(cfg.APIKey)
		}
		info := fmt.Sprintf("配置文件: %s\n\n  provider: %s\n  model:    %s\n  base_url: %s\n  api_key:  %s",
			cfgPath,
			valueOrDefault(cfg.Provider, "(未设置)"),
			valueOrDefault(cfg.Model, "(未设置)"),
			valueOrDefault(cfg.BaseURL, "(未设置)"),
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
				content: fmt.Sprintf("未知配置项: %s (支持: provider, model, base_url, api_key)", key),
			})
			m.refreshViewport()
			return m, nil
		}

		if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
			m.messages = append(m.messages, chatMessage{
				kind:    msgError,
				content: fmt.Sprintf("保存配置失败: %v", err),
			})
		} else {
			display := value
			if key == "api_key" || key == "apikey" {
				display = maskKey(value)
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("已设置 %s = %s\n提示: 部分设置需要重启 moss 或使用 /model 切换后生效。", key, display),
			})
		}
		m.refreshViewport()
		return m, nil
	}

	m.messages = append(m.messages, chatMessage{
		kind:    msgSystem,
		content: "用法:\n  /config              查看当前配置\n  /config set <key> <value>  设置配置项",
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
