package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/skill"
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
	APIType            string
	ProviderName       string
	Provider           string
	Model              string
	Workspace          string
	Trust              string
	SessionStoreDir    string
	BaseURL            string
	APIKey             string
	BuildKernel        func(wsDir, trust, apiType, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error)
	AfterBoot          func(ctx context.Context, k *kernel.Kernel, io port.UserIO) error
	BuildSystemPrompt  func(workspace string) string
	BuildSessionConfig func(workspace, trust, systemPrompt string) session.SessionConfig
	ScheduleController runtime.ScheduleController
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
	k           *kernel.Kernel
	sess        *session.Session
	store       session.SessionStore
	ctx         context.Context
	cancel      context.CancelFunc
	runCancel   context.CancelFunc
	bridge      *BridgeIO
	trust       string
	permissions map[string]string
	mu          sync.Mutex
	running     bool // 是否正在执行 loop
}

func renderSkillsSummary(agent *agentState, workspace string) string {
	manifests := runtime.SkillManifests(agent.k)
	if len(manifests) == 0 {
		manifests = skill.DiscoverSkillManifests(workspace)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })

	var sb strings.Builder
	if len(manifests) == 0 {
		sb.WriteString("No user-installed SKILL.md skills were found.")
	} else {
		sb.WriteString("Discovered user skills:\n")
		for _, mf := range manifests {
			loaded := "inactive"
			if _, ok := runtime.SkillsManager(agent.k).Get(mf.Name); ok {
				loaded = "active"
			}
			if strings.TrimSpace(mf.Description) == "" {
				sb.WriteString(fmt.Sprintf("  • %s [%s]\n", mf.Name, loaded))
			} else {
				sb.WriteString(fmt.Sprintf("  • %s [%s] — %s\n", mf.Name, loaded, mf.Description))
			}
		}
	}

	builtinSet := make(map[string]struct{})
	for _, name := range runtime.RegisteredBuiltinToolNames(agent.k.Sandbox(), agent.k.Workspace(), agent.k.Executor()) {
		builtinSet[name] = struct{}{}
	}
	var builtinNames []string
	for _, spec := range agent.k.ToolRegistry().List() {
		if _, ok := builtinSet[spec.Name]; ok {
			builtinNames = append(builtinNames, spec.Name)
		}
	}
	sort.Strings(builtinNames)
	if len(builtinNames) > 0 {
		sb.WriteString("\n\nRuntime builtin tools:\n")
		for _, name := range builtinNames {
			sb.WriteString("  - " + name + "\n")
		}
	}

	return "```text\n" + sb.String() + "\n```"
}

func (a *agentState) sessionSummary() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return "No active session."
	}
	dialogCount := 0
	for _, msg := range a.sess.Messages {
		if msg.Role != port.RoleSystem {
			dialogCount++
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Session: %s\n", a.sess.ID))
	b.WriteString(fmt.Sprintf("Status: %s\n", a.sess.Status))
	b.WriteString(fmt.Sprintf("Messages: %d (dialog: %d)\n", len(a.sess.Messages), dialogCount))
	b.WriteString(fmt.Sprintf("Budget: steps %d/%d, tokens %d/%d",
		a.sess.Budget.UsedSteps, a.sess.Budget.MaxSteps,
		a.sess.Budget.UsedTokens, a.sess.Budget.MaxTokens,
	))
	if v, ok := a.sess.GetState("last_offload_snapshot"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload snapshot: %v", v))
	}
	if v, ok := a.sess.GetState("last_offload_at"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload time: %v", v))
	}
	b.WriteString(fmt.Sprintf("\nTrust: %s", a.trust))
	return b.String()
}

func (a *agentState) listPersistedSessions(limit int) (string, error) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	summaries, err := store.List(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].CreatedAt > summaries[j].CreatedAt })
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	if len(summaries) == 0 {
		return "No persisted sessions found.", nil
	}
	var b strings.Builder
	b.WriteString("Persisted sessions:\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("- %s | %s | %s | steps=%d | created=%s\n", s.ID, s.Status, s.Mode, s.Steps, s.CreatedAt))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (a *agentState) restoreSession(sessionID string) (string, error) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	loaded, err := store.Load(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if loaded == nil {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	a.mu.Lock()
	a.sess = loaded
	a.mu.Unlock()
	return fmt.Sprintf("Restored session %s (%s, steps=%d, messages=%d).", loaded.ID, loaded.Status, loaded.Budget.UsedSteps, len(loaded.Messages)), nil
}

func (a *agentState) setPermission(toolName, mode string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if toolName == "" {
		return "", fmt.Errorf("tool name is required")
	}
	switch mode {
	case "allow", "ask", "deny", "reset":
	default:
		return "", fmt.Errorf("mode must be allow|ask|deny|reset")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.permissions == nil {
		a.permissions = map[string]string{}
	}
	if mode == "reset" {
		delete(a.permissions, toolName)
		return fmt.Sprintf("Permission reset for %s.", toolName), nil
	}
	a.permissions[toolName] = mode
	return fmt.Sprintf("Permission updated: %s -> %s", toolName, mode), nil
}

func (a *agentState) permissionSummary() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Trust: %s\n", a.trust))
	if len(a.permissions) == 0 {
		b.WriteString("Overrides: none")
		return b.String()
	}
	keys := make([]string, 0, len(a.permissions))
	for k := range a.permissions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("Overrides:\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, a.permissions[k]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *agentState) permissionOverrideMiddleware() middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase == middleware.BeforeToolCall && mc.Tool != nil {
			a.mu.Lock()
			mode := a.permissions[mc.Tool.Name]
			a.mu.Unlock()
			switch mode {
			case "deny":
				return builtins.ErrDenied
			case "ask":
				if mc.IO != nil {
					approval := &port.ApprovalRequest{
						ID:          fmt.Sprintf("approval-%d", time.Now().UnixNano()),
						Kind:        port.ApprovalKindTool,
						SessionID:   mc.Session.ID,
						ToolName:    mc.Tool.Name,
						Risk:        string(mc.Tool.Risk),
						Prompt:      "Allow tool " + mc.Tool.Name + "?",
						Reason:      "permission override requires approval",
						Input:       append(json.RawMessage(nil), mc.Input...),
						RequestedAt: time.Now().UTC(),
					}
					resp, err := mc.IO.Ask(ctx, port.InputRequest{
						Type:     port.InputConfirm,
						Prompt:   approval.Prompt,
						Approval: approval,
						Meta: map[string]any{
							"tool":        mc.Tool.Name,
							"input":       mc.Input,
							"approval_id": approval.ID,
							"reason":      approval.Reason,
							"risk":        approval.Risk,
						},
					})
					if err != nil {
						return err
					}
					if resp.Decision != nil {
						resp.Approved = resp.Decision.Approved
					}
					if !resp.Approved {
						return builtins.ErrDenied
					}
				}
				return next(ctx)
			case "allow":
				return next(ctx)
			}
		}
		return next(ctx)
	}
}

func (a *agentState) invokeTool(ctx context.Context, name string, input any) (json.RawMessage, error) {
	_, handler, ok := a.k.ToolRegistry().Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not available in current runtime", name)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool input: %w", err)
	}
	return handler(ctx, raw)
}

func formatJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func (a *agentState) offloadContext(keepRecent int, note string) (string, error) {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return "", errors.New("no active session")
	}
	if keepRecent <= 0 {
		keepRecent = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "offload_context", map[string]any{
		"session_id":  sess.ID,
		"keep_recent": keepRecent,
		"note":        note,
	})
	if err != nil {
		return "", err
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return formatJSON(raw), nil
	}
	status, _ := resp["status"].(string)
	switch status {
	case "noop":
		return fmt.Sprintf("No offload needed: conversation length does not exceed keep_recent=%d.", keepRecent), nil
	case "offloaded":
		return "Context offload completed.\n" + formatJSON(raw), nil
	default:
		return formatJSON(raw), nil
	}
}

type taskView struct {
	ID        string `json:"id"`
	AgentName string `json:"agent_name"`
	Goal      string `json:"goal"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

func formatTaskList(raw json.RawMessage) string {
	var payload struct {
		Tasks []taskView `json:"tasks"`
		Count int        `json:"count"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return formatJSON(raw)
	}
	if len(payload.Tasks) == 0 {
		return "No matching background tasks."
	}
	sort.Slice(payload.Tasks, func(i, j int) bool { return payload.Tasks[i].ID < payload.Tasks[j].ID })
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tasks (%d):\n", payload.Count))
	for _, t := range payload.Tasks {
		line := fmt.Sprintf("- %s | %s | %s", t.ID, t.AgentName, t.Status)
		if t.Goal != "" {
			line += " | " + t.Goal
		}
		if t.Error != "" {
			line += " | err: " + t.Error
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *agentState) listTasks(status string, limit int) (string, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	input := map[string]any{
		"status": strings.TrimSpace(status),
		"limit":  limit,
	}
	raw, err := a.invokeTool(ctx, "list_tasks", input)
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":   "list",
			"status": strings.TrimSpace(status),
			"limit":  limit,
		})
		if err != nil {
			return "", err
		}
	}
	return formatTaskList(raw), nil
}

func (a *agentState) queryTask(taskID string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "query_agent", map[string]any{"task_id": taskID})
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":    "query",
			"task_id": taskID,
		})
		if err != nil {
			return "", err
		}
	}
	return formatJSON(raw), nil
}

func (a *agentState) cancelTask(taskID, reason string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "cancel_task", map[string]any{
		"task_id": taskID,
		"reason":  reason,
	})
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":    "cancel",
			"task_id": taskID,
			"reason":  reason,
		})
		if err != nil {
			return "", err
		}
	}
	return formatJSON(raw), nil
}

// appendAndRun 追加用户消息到 session 并重新执行 agent loop。
func (a *agentState) appendAndRun(text string) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return // 防止重复执行
	}
	a.running = true
	runCtx, runCancel := context.WithCancel(a.ctx)
	a.runCancel = runCancel
	a.mu.Unlock()

	a.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: text})

	result, err := a.k.Run(runCtx, a.sess)

	a.mu.Lock()
	a.running = false
	a.runCancel = nil
	a.mu.Unlock()

	if a.bridge.program != nil {
		msg := sessionResultMsg{err: err}
		if result != nil {
			msg.output = result.Output
		}
		a.bridge.program.Send(msg)
	}
}

func (a *agentState) cancelCurrentRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.runCancel == nil {
		return false
	}
	a.runCancel()
	return true
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
	defaultProvider := configpkg.NormalizeProviderIdentity(cfg.APIType, cfg.Provider, cfg.ProviderName)
	defaultAPIType := defaultProvider.EffectiveAPIType()
	defaultProviderName := defaultProvider.DisplayName()
	if defaultAPIType != "" && cfg.Workspace != "" {
		wCfg := WelcomeConfig{
			APIType:      defaultAPIType,
			ProviderName: defaultProviderName,
			Model:        cfg.Model,
			Workspace:    cfg.Workspace,
		}
		m.state = stateChat
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity(wCfg.APIType, wCfg.Provider, wCfg.ProviderName).Label(), wCfg.Model, wCfg.Workspace)
		m.initCmd = initKernelCmd(cfg, wCfg, bridge)
	} else {
		m.state = stateWelcome
		m.welcome = newWelcomeModel(defaultAPIType, defaultProviderName, cfg.Model, cfg.Workspace)
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	bridge.SetProgram(p)

	_, err := p.Run()
	return err
}

func (m appModel) Init() tea.Cmd {
	if m.state == stateChat {
		// 跳过 Welcome 直接进入 Chat，同时启动 textarea 光标闪烁和 kernel 初始化
		if strings.TrimSpace(m.config.Trust) != "" {
			m.chat.trust = m.config.Trust
		}
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
		m.config.APIType = cfg.APIType
		m.config.Provider = cfg.Provider
		m.config.ProviderName = cfg.ProviderName
		m.config.Model = cfg.Model
		m.config.Workspace = cfg.Workspace
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity(cfg.APIType, cfg.Provider, cfg.ProviderName).Label(), cfg.Model, cfg.Workspace)
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
		identity := configpkg.NormalizeProviderIdentity(m.config.APIType, m.config.Provider, m.config.ProviderName)
		wCfg := WelcomeConfig{
			APIType:      identity.APIType,
			ProviderName: identity.Name,
			Provider:     identity.Provider,
			Model:        sm.model,
			Workspace:    m.config.Workspace,
		}
		m.chat.provider = identity.Label()
		m.chat.model = sm.model
		m.chat.trust = m.config.Trust
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	// 切换 trust：关闭旧 kernel，用新 trust 重建
	if st, ok := msg.(switchTrustMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		m.agent = nil
		m.chat.sendFn = nil
		m.config.Trust = st.trust
		identity := configpkg.NormalizeProviderIdentity(m.config.APIType, m.config.Provider, m.config.ProviderName)
		wCfg := WelcomeConfig{
			APIType:      identity.APIType,
			ProviderName: identity.Name,
			Provider:     identity.Provider,
			Model:        m.config.Model,
			Workspace:    m.config.Workspace,
		}
		m.chat.trust = st.trust
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	// kernel 就绪：设置 sendFn 为多轮复用 session 的方式
	if ready, ok := msg.(kernelReadyMsg); ok {
		m.agent = ready.agent
		agent := ready.agent
		m.chat.sendFn = func(text string) {
			go agent.appendAndRun(text)
		}
		m.chat.cancelRunFn = agent.cancelCurrentRun
		m.chat.trust = m.config.Trust
		m.chat.sessionInfoFn = agent.sessionSummary
		m.chat.sessionListFn = func(limit int) (string, error) {
			return agent.listPersistedSessions(limit)
		}
		m.chat.sessionRestoreFn = func(sessionID string) (string, error) {
			return agent.restoreSession(sessionID)
		}
		m.chat.offloadFn = func(keepRecent int, note string) (string, error) {
			return agent.offloadContext(keepRecent, note)
		}
		m.chat.taskListFn = func(status string, limit int) (string, error) {
			return agent.listTasks(status, limit)
		}
		m.chat.taskQueryFn = func(taskID string) (string, error) {
			return agent.queryTask(taskID)
		}
		m.chat.taskCancelFn = func(taskID, reason string) (string, error) {
			return agent.cancelTask(taskID, reason)
		}
		m.chat.scheduleCtrl = m.config.ScheduleController
		m.chat.permissionSummaryFn = agent.permissionSummary
		m.chat.setPermissionFn = agent.setPermission
		m.chat.gitRunFn = func(cmd string, args []string) (string, error) {
			raw, err := agent.invokeTool(agent.ctx, "run_command", map[string]any{
				"command": cmd,
				"args":    args,
			})
			if err != nil {
				return "", err
			}
			return formatJSON(raw), nil
		}
		m.chat.skillListFn = func() string {
			return renderSkillsSummary(agent, m.config.Workspace)
		}
		connInfo := m.chat.provider
		if m.config.Model != "" {
			m.chat.model = m.config.Model
			connInfo += " (" + m.config.Model + ")"
		}
		if m.config.Trust != "" {
			connInfo += " [" + m.config.Trust + "]"
		}
		m.chat.streaming = false
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Connected to %s", connInfo),
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
		apiType := strings.ToLower(configpkg.NormalizeProviderIdentity(wCfg.APIType, wCfg.Provider, wCfg.ProviderName).EffectiveAPIType())

		k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, apiType, wCfg.Model, cfg.APIKey, cfg.BaseURL, bridge)
		if err != nil {
			return sessionResultMsg{err: fmt.Errorf("failed to initialize kernel: %w", err)}
		}

		ctx, cancel := context.WithCancel(context.Background())
		if err := k.Boot(ctx); err != nil {
			cancel()
			return sessionResultMsg{err: fmt.Errorf("failed to boot kernel: %w", err)}
		}
		var store session.SessionStore
		if strings.TrimSpace(cfg.SessionStoreDir) != "" {
			store, _ = session.NewFileStore(cfg.SessionStoreDir)
		}
		if cfg.AfterBoot != nil {
			if err := cfg.AfterBoot(ctx, k, bridge); err != nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("failed to initialize runtime: %w", err)}
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
			return sessionResultMsg{err: fmt.Errorf("failed to create session: %w", err)}
		}

		agent := &agentState{
			k:           k,
			sess:        sess,
			store:       store,
			ctx:         ctx,
			cancel:      cancel,
			bridge:      bridge,
			trust:       cfg.Trust,
			permissions: map[string]string{},
		}

		k.Middleware().Use(agent.permissionOverrideMiddleware())

		return kernelReadyMsg{agent: agent}
	}
}
