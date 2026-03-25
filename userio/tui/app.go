package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
	Provider           string
	Model              string
	Workspace          string
	Trust              string
	BaseURL            string
	APIKey             string
	BuildKernel        func(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error)
	AfterBoot          func(ctx context.Context, k *kernel.Kernel, io port.UserIO) error
	BuildSystemPrompt  func(workspace string) string
	BuildSessionConfig func(workspace, trust, systemPrompt string) session.SessionConfig
	SidebarTitle       string
	RenderSidebar      func() string
}

// kernelReadyMsg 表示 kernel 已初始化并启动，session 已创建。
type kernelReadyMsg struct {
	agent *agentState
}

// agentState 管理 kernel 和 session 的长生命周期状态（跨 Bubble Tea 值传递）。
// 使用指针共享，避免 Bubble Tea 值语义问题。
type agentState struct {
	k       *kernel.Kernel
	sess    *session.Session
	ctx     context.Context
	cancel  context.CancelFunc
	bridge  *BridgeIO
	trust   string
	mu      sync.Mutex
	running bool // 是否正在执行 loop
}

// appendAndRun 追加用户消息到 session 并重新执行 agent loop。
func (a *agentState) appendAndRun(text string) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return // 防止重复执行
	}
	a.running = true
	a.mu.Unlock()

	a.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: text})

	result, err := a.k.Run(a.ctx, a.sess)

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	if a.bridge.program != nil {
		msg := sessionResultMsg{err: err}
		if result != nil {
			msg.output = result.Output
		}
		a.bridge.program.Send(msg)
	}
}

// appModel 是顶层 Bubble Tea Model。
type appModel struct {
	state    appState
	welcome  welcomeModel
	chat     chatModel
	config   Config
	bridgeIO *BridgeIO
	agent    *agentState // 共享指针，跨值传递保持一致
	width    int
	height   int
	initCmd  tea.Cmd // 直接进入 chat 时的初始化命令
}

// Run 启动 TUI 应用。
func Run(cfg Config) error {
	bridge := NewBridgeIO()

	m := appModel{
		config:   cfg,
		bridgeIO: bridge,
	}

	// 如果 CLI 已提供足够配置，跳过 Welcome 直接进入 Chat
	if cfg.Provider != "" && cfg.Workspace != "" {
		wCfg := WelcomeConfig{
			Provider:  cfg.Provider,
			Model:     cfg.Model,
			Workspace: cfg.Workspace,
		}
		m.state = stateChat
		m.chat = newChatModel(wCfg.Provider, wCfg.Workspace)
		m.chat.sidebarTitle = cfg.SidebarTitle
		m.chat.renderSidebar = cfg.RenderSidebar
		m.initCmd = initKernelCmd(cfg, wCfg, bridge)
	} else {
		m.state = stateWelcome
		m.welcome = newWelcomeModel(cfg.Provider, cfg.Model, cfg.Workspace)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	bridge.SetProgram(p)

	_, err := p.Run()
	return err
}

func (m appModel) Init() tea.Cmd {
	if m.state == stateChat {
		// 跳过 Welcome 直接进入 Chat，同时启动 textarea 光标闪烁和 kernel 初始化
		return tea.Batch(m.chat.Init(), m.initCmd)
	}
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
		// 持久化用户选择的 provider/model 到 ~/.moss/config.yaml
		saveWelcomeConfig(cfg)
		m.chat = newChatModel(cfg.Provider, cfg.Workspace)
		m.chat.sidebarTitle = m.config.SidebarTitle
		m.chat.renderSidebar = m.config.RenderSidebar
		m.state = stateChat

		// 将当前窗口尺寸传递给 chatModel，避免它因未收到 WindowSizeMsg 而卡在 "加载中"
		if m.width > 0 && m.height > 0 {
			m.chat.width = m.width
			m.chat.height = m.height
			m.chat.recalcLayout()
		}

		return m, initKernelCmd(m.config, cfg, m.bridgeIO)
	}

	return m, cmd
}

func (m appModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 取消并退出
	if _, ok := msg.(cancelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		return m, tea.Quit
	}

	// 切换模型：关闭旧 kernel，用新 model 重建
	if sm, ok := msg.(switchModelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		m.agent = nil
		m.chat.sendFn = nil

		// 更新 config 中的 model
		m.config.Model = sm.model
		wCfg := WelcomeConfig{
			Provider:  m.config.Provider,
			Model:     sm.model,
			Workspace: m.config.Workspace,
		}
		m.chat.provider = m.config.Provider
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	// kernel 就绪：设置 sendFn 为多轮复用 session 的方式
	if ready, ok := msg.(kernelReadyMsg); ok {
		m.agent = ready.agent
		agent := ready.agent
		m.chat.sendFn = func(text string) {
			go agent.appendAndRun(text)
		}
		m.chat.skillListFn = func() string {
			skills := agent.k.SkillManager().List()
			if len(skills) == 0 {
				return "暂无已加载的 skill。"
			}
			var sb strings.Builder
			sb.WriteString("已加载的 skills:\n")
			for _, s := range skills {
				sb.WriteString(fmt.Sprintf("  • %s v%s — %s\n", s.Name, s.Version, s.Description))
				if len(s.Tools) > 0 {
					sb.WriteString(fmt.Sprintf("    工具: %s\n", strings.Join(s.Tools, ", ")))
				}
			}
			return sb.String()
		}
		connInfo := m.chat.provider
		if m.config.Model != "" {
			connInfo += " (" + m.config.Model + ")"
		}
		m.chat.streaming = false
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("已连接到 %s", connInfo),
		})
		m.chat.refreshViewport()
		return m, nil
	}

	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	return m, cmd
}

// initKernelCmd 异步创建 kernel + session。
func initKernelCmd(cfg Config, wCfg WelcomeConfig, bridge *BridgeIO) tea.Cmd {
	return func() tea.Msg {
		provider := strings.ToLower(wCfg.Provider)

		k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, provider, wCfg.Model, cfg.APIKey, cfg.BaseURL, bridge)
		if err != nil {
			return sessionResultMsg{err: fmt.Errorf("初始化 kernel 失败: %w", err)}
		}

		ctx, cancel := context.WithCancel(context.Background())
		if err := k.Boot(ctx); err != nil {
			cancel()
			return sessionResultMsg{err: fmt.Errorf("启动 kernel 失败: %w", err)}
		}
		if cfg.AfterBoot != nil {
			if err := cfg.AfterBoot(ctx, k, bridge); err != nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("初始化运行时失败: %w", err)}
			}
		}

		// 创建持久 session，注入 system prompt（Kernel 自动合并 skill additions）
		sysPrompt := buildSystemPrompt(wCfg.Workspace)
		if cfg.BuildSystemPrompt != nil {
			sysPrompt = cfg.BuildSystemPrompt(wCfg.Workspace)
		}
		sessCfg := session.SessionConfig{
			Goal:         "interactive",
			Mode:         "interactive",
			TrustLevel:   cfg.Trust,
			MaxSteps:     200,
			SystemPrompt: sysPrompt,
		}
		if cfg.BuildSessionConfig != nil {
			sessCfg = cfg.BuildSessionConfig(wCfg.Workspace, cfg.Trust, sysPrompt)
			if sessCfg.SystemPrompt == "" {
				sessCfg.SystemPrompt = sysPrompt
			}
			if sessCfg.TrustLevel == "" {
				sessCfg.TrustLevel = cfg.Trust
			}
			if sessCfg.Goal == "" {
				sessCfg.Goal = "interactive"
			}
			if sessCfg.Mode == "" {
				sessCfg.Mode = "interactive"
			}
			if sessCfg.MaxSteps == 0 {
				sessCfg.MaxSteps = 200
			}
		}

		sess, err := k.NewSession(ctx, sessCfg)
		if err != nil {
			cancel()
			return sessionResultMsg{err: fmt.Errorf("创建 session 失败: %w", err)}
		}

		agent := &agentState{
			k:      k,
			sess:   sess,
			ctx:    ctx,
			cancel: cancel,
			bridge: bridge,
			trust:  cfg.Trust,
		}

		return kernelReadyMsg{agent: agent}
	}
}
