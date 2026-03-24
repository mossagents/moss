package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagi/moss/kernel/port"
)

// sessionResultMsg 表示 agent session 结束。
type sessionResultMsg struct {
	output string
	err    error
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
	sendFn   func(string) // 发送用户消息给 agent
	pendAsk  *bridgeAsk   // 当前阻塞的 Ask 请求
	finished bool         // session 已结束
	result   string       // 最终结果

	// 配置显示
	provider  string
	workspace string
}

func newChatModel(provider, workspace string) chatModel {
	ta := textarea.New()
	ta.Placeholder = "输入消息... (Enter 发送, Ctrl+C 退出)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")

	return chatModel{
		textarea:  ta,
		provider:  provider,
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
			return m, tea.Quit
		case "enter":
			return m.handleSend()
		}

	case bridgeMsg:
		return m.handleBridge(msg)

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
	content := renderAllMessages(m.messages, m.width)
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
		m.viewport = viewport.New(m.width, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
	}

	m.textarea.SetWidth(m.width - 4)
	m.refreshViewport()
}

func (m chatModel) View() string {
	if !m.ready {
		return "加载中..."
	}

	var b strings.Builder

	// 顶栏
	header := titleStyle.Render("🌿 moss")
	info := statusBarStyle.Render(fmt.Sprintf("  %s │ %s", m.provider, m.workspace))
	b.WriteString(header + info + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// 消息区
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// 输入区
	if m.pendAsk != nil {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	} else if m.streaming {
		b.WriteString(mutedStyle.Render("  ● 思考中..."))
	} else if m.finished {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	} else {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// 底部状态
	status := mutedStyle.Render("Ctrl+C 退出")
	if m.pendAsk != nil {
		status = mutedStyle.Render("输入回复后按 Enter 发送 │ Ctrl+C 退出")
	}
	b.WriteString(status)

	return b.String()
}
