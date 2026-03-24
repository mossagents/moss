package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
)

const appVersion = "0.3.0"

// appState 表示 TUI 应用的状态。
type appState int

const (
	stateWelcome appState = iota
	stateChat
)

// Config 是启动 TUI 的配置。
type Config struct {
	Provider    string
	Workspace   string
	Trust       string
	BuildKernel func(wsDir, trust, provider, model string, io port.UserIO) (*kernel.Kernel, error)
}

// kernelReadyMsg 表示 kernel 已初始化并启动。
// 通过消息传递避免在 tea.Cmd 闭包中修改值类型 model。
type kernelReadyMsg struct {
	k      *kernel.Kernel
	ctx    context.Context
	cancel context.CancelFunc
}

// appModel 是顶层 Bubble Tea Model。
type appModel struct {
	state    appState
	welcome  welcomeModel
	chat     chatModel
	config   Config
	bridgeIO *BridgeIO
	k        *kernel.Kernel
	cancel   context.CancelFunc
	width    int
	height   int
}

// Run 启动 TUI 应用。
func Run(cfg Config) error {
	bridge := NewBridgeIO()

	m := appModel{
		state:    stateWelcome,
		welcome:  newWelcomeModel(cfg.Provider, cfg.Workspace),
		config:   cfg,
		bridgeIO: bridge,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	bridge.SetProgram(p)

	_, err := p.Run()
	return err
}

func (m appModel) Init() tea.Cmd {
	return m.welcome.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 全局窗口大小
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = ws.Width
		m.height = ws.Height
	}

	switch m.state {
	case stateWelcome:
		return m.updateWelcome(msg)
	case stateChat:
		return m.updateChat(msg)
	}
	return m, nil
}

func (m appModel) View() string {
	switch m.state {
	case stateWelcome:
		return m.welcome.View()
	case stateChat:
		return m.chat.View()
	}
	return ""
}

func (m appModel) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.welcome, cmd = m.welcome.Update(msg)

	if m.welcome.cancelled {
		return m, tea.Quit
	}

	if m.welcome.confirmed {
		cfg := m.welcome.config()
		m.chat = newChatModel(cfg.Provider, cfg.Workspace)
		m.state = stateChat

		// 启动 kernel 初始化（异步 Cmd，结果通过 kernelReadyMsg 传回）
		return m, initKernelCmd(m.config, cfg, m.bridgeIO)
	}

	return m, cmd
}

func (m appModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 在 app 层处理 kernel 就绪消息，设置 chat.sendFn
	if ready, ok := msg.(kernelReadyMsg); ok {
		m.k = ready.k
		m.cancel = ready.cancel
		k := ready.k
		ctx := ready.ctx
		trust := m.config.Trust
		bridge := m.bridgeIO
		m.chat.sendFn = func(text string) {
			go runSession(ctx, k, trust, bridge, text)
		}
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgAssistant,
			content: fmt.Sprintf("已连接到 %s，输入消息开始对话。", m.chat.provider),
		})
		m.chat.refreshViewport()
		return m, nil
	}

	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	return m, cmd
}

// initKernelCmd 返回一个异步 Cmd，完成 kernel 创建和启动。
// 不修改 model，通过返回 kernelReadyMsg 或 sessionResultMsg 传递结果。
func initKernelCmd(cfg Config, wCfg WelcomeConfig, bridge *BridgeIO) tea.Cmd {
	return func() tea.Msg {
		provider := strings.ToLower(wCfg.Provider)

		k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, provider, "", bridge)
		if err != nil {
			return sessionResultMsg{err: fmt.Errorf("初始化 kernel 失败: %w", err)}
		}

		ctx, cancel := context.WithCancel(context.Background())
		if err := k.Boot(ctx); err != nil {
			cancel()
			return sessionResultMsg{err: fmt.Errorf("启动 kernel 失败: %w", err)}
		}

		return kernelReadyMsg{k: k, ctx: ctx, cancel: cancel}
	}
}

// runSession 在后台运行 agent session（独立函数，不依赖 model 指针）。
func runSession(ctx context.Context, k *kernel.Kernel, trust string, bridge *BridgeIO, goal string) {
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:       goal,
		Mode:       "interactive",
		TrustLevel: trust,
		MaxSteps:   50,
	})
	if err != nil {
		bridge.Send(ctx, port.OutputMessage{
			Type:    port.OutputText,
			Content: fmt.Sprintf("创建 session 失败: %v", err),
		})
		return
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: goal})

	result, err := k.Run(ctx, sess)
	if bridge.program != nil {
		msg := sessionResultMsg{err: err}
		if result != nil {
			msg.output = result.Output
		}
		bridge.program.Send(msg)
	}
}
