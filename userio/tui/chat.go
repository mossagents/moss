package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

const (
	maxInputHistory = 500
)

var writeClipboard = clipboard.WriteAll

// sessionResultMsg 表示 agent session 结束。
type sessionResultMsg struct {
	output       string
	outputMedia  []port.ContentPart
	trace        *product.RunTraceSummary
	traceSummary string
	err          error
}

// cancelMsg 通知应用退出并清理资源。
type cancelMsg struct{}

// switchModelMsg 通知 app 切换模型。
type switchModelMsg struct {
	provider     string
	providerName string
	model        string
	auto         bool
}

// switchTrustMsg 通知 app 切换 trust level。
type switchTrustMsg struct {
	trust string
}

// switchApprovalMsg 通知 app 切换 approval mode。
type switchApprovalMsg struct {
	mode string
}

type switchProfileMsg struct {
	profile     string
	prompt      string
	displayText string
}

type uiTickMsg struct{}

type threadSwitchResultMsg struct {
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
	sendFn                func(string, []port.ContentPart) // 发送用户消息给 agent
	cancelRunFn           func() bool                      // 取消当前运行中的任务
	skillListFn           func() string                    // 查询已加载 skills
	sessionInfoFn         func() string
	offloadFn             func(keepRecent int, note string) (string, error)
	taskListFn            func(status string, limit int) (string, error)
	taskQueryFn           func(taskID string) (string, error)
	taskCancelFn          func(taskID, reason string) (string, error)
	scheduleCtrl          runtime.ScheduleController
	sessionListFn         func(limit int) (string, error)
	changeListFn          func(limit int) (string, error)
	changeShowFn          func(changeID string) (string, error)
	applyChangeFn         func(patchFile, summary string) (string, error)
	rollbackChangeFn      func(changeID string) (string, error)
	checkpointListFn      func(limit int) (string, error)
	checkpointShowFn      func(checkpointID string) (string, error)
	checkpointCreateFn    func(note string) (string, error)
	checkpointForkFn      func(sourceKind, sourceID string, restoreWorktree bool) (string, error)
	checkpointReplayFn    func(checkpointID, mode string, restoreWorktree bool) (string, error)
	sessionRestoreFn      func(sessionID string) (string, error)
	newSessionFn          func() (string, error)
	gitRunFn              func(cmd string, args []string) (string, error)
	permissionSummaryFn   func() string
	setPermissionFn       func(toolName, mode string) (string, error)
	debugConfigFn         func() string
	debugPromptFn         func() string
	refreshSystemPromptFn func() error
	pendAsk               *bridgeAsk // 当前阻塞的 Ask 请求
	askForm               *askFormState
	scheduleBrowser       *scheduleBrowserState
	modelPicker           *modelPickerState
	themePicker           *themePickerState
	statuslinePicker      *statuslinePickerState
	mcpPicker             *mcpPickerState
	helpPicker            *helpPickerState
	reviewPicker          *reviewPickerState
	resumePicker          *resumePickerState
	forkPicker            *forkPickerState
	agentPicker           *agentPickerState
	mentionPicker         *mentionPickerState
	transcriptOverlay     *transcriptOverlayState
	overlays              *overlayStack
	finished              bool   // session 已结束
	result                string // 最终结果
	lastTrace             *product.RunTraceSummary
	currentSessionID      string
	progress              executionProgressState
	progressTrail         []executionProgressState
	lastThinkingSignature string
	approvalRules         map[string][]approvalMemoryRule
	projectApprovalRules  []approvalMemoryRule
	debugPromptPreview    bool

	// 工具输出折叠
	toolCollapsed bool // true 时折叠 tool start/result 消息

	// 配置显示
	provider             string
	providerID           string
	providerName         string
	startupBanner        string
	sidebarTitle         string
	renderSidebarFn      func() string
	model                string
	modelAuto            bool
	workspace            string
	trust                string
	profile              string
	approvalMode         string
	theme                string
	personality          string
	fastMode             bool
	statusLineItems      []string
	experimentalFeatures []string
	customCommands       []product.CustomCommand
	discoveredSkills     []string

	queuedInputs       []string
	queuedParts        [][]port.ContentPart
	pendingAttachments []composerAttachment

	inputHistory  []string
	historyCursor int
	historyDraft  string
	historyPath   string
	slashHints    []string
	slashPopup    *slashPopupState

	now          func() time.Time
	lastEscAt    time.Time
	lastCtrlC    time.Time
	runStartedAt time.Time
}

func newChatModel(provider, model, workspace string) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send, Shift+Enter/Alt+Enter/Ctrl+J for newline)"
	ta.Prompt = ""
	ta.Focus()
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	// Bubble Tea does not reliably expose Shift+Enter on all terminals, so keep it
	// as a best-effort binding and add portable fallbacks for multiline input.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	theme := resolveThemeName(os.Getenv("MOSSCODE_THEME"))
	personality := product.PersonalityFriendly
	var fastMode bool
	statusLineItems := append([]string(nil), defaultStatusLineItems...)
	experimentalFeatures := product.DefaultExperimentalFeatures()
	if prefs, err := product.LoadTUIConfig(); err == nil {
		if envTheme := strings.TrimSpace(os.Getenv("MOSSCODE_THEME")); envTheme == "" {
			theme = resolveThemeName(prefs.Theme)
		}
		if normalized := product.NormalizePersonality(prefs.Personality); normalized != "" {
			personality = normalized
		}
		if prefs.FastMode != nil {
			fastMode = *prefs.FastMode
		}
		if len(prefs.StatusLine) > 0 {
			statusLineItems = append([]string(nil), prefs.StatusLine...)
		}
		if len(prefs.Experimental) > 0 {
			experimentalFeatures = append([]string(nil), prefs.Experimental...)
		}
	}
	projectApprovalRules := []approvalMemoryRule(nil)
	if projectPrefs, err := product.LoadProjectTUIConfig(workspace); err == nil {
		projectApprovalRules = approvalProjectRulesFromConfig(projectPrefs)
	}
	applyTheme(theme)

	return chatModel{
		textarea:             ta,
		provider:             provider,
		model:                model,
		modelAuto:            strings.TrimSpace(model) == "",
		workspace:            workspace,
		trust:                "trusted",
		theme:                theme,
		personality:          personality,
		fastMode:             fastMode,
		statusLineItems:      statusLineItems,
		experimentalFeatures: experimentalFeatures,
		toolCollapsed:        true,
		approvalRules:        map[string][]approvalMemoryRule{},
		projectApprovalRules: projectApprovalRules,
		overlays:             newOverlayStack(),
		inputHistory:         loadInputHistory(defaultHistoryPath(), maxInputHistory),
		historyPath:          defaultHistoryPath(),
		now:                  time.Now,
	}
}

func (m *chatModel) setProviderIdentity(provider, providerName string) {
	identity := config.NormalizeProviderIdentity("", provider, providerName)
	m.providerID = identity.Provider
	m.providerName = identity.Name
	m.provider = identity.Label()
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, uiTickCmd())
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
		m.messages = append(m.messages, chatMessage{
			kind:    msgUser,
			content: text,
			meta:    map[string]any{"timestamp": m.now().UTC()},
		})
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
	displayText, runText, parts, err := buildComposerSubmission(text, m.workspace, m.pendingAttachments)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to build attachments: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	return m.dispatchUserSubmission(displayText, runText, parts)
}

func (m chatModel) startThreadSwitch(statusText string, run func() (string, error)) (chatModel, tea.Cmd) {
	if strings.TrimSpace(statusText) != "" {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: statusText})
	}
	m.streaming = true
	m.runStartedAt = m.now().UTC()
	m.refreshViewport()
	return m, func() tea.Msg {
		out, err := run()
		if err != nil {
			return threadSwitchResultMsg{err: err}
		}
		return threadSwitchResultMsg{output: out}
	}
}

func (m chatModel) handleBridge(msg bridgeMsg) (chatModel, tea.Cmd) {
	if msg.output != nil {
		o := msg.output
		switch o.Type {
		case port.OutputText:
			m.messages = append(m.messages, chatMessage{
				kind:    msgAssistant,
				content: o.Content,
				meta:    map[string]any{"timestamp": m.now().UTC()},
			})
		case port.OutputStream:
			m.appendStream(o.Content)
		case port.OutputStreamEnd:
			m.streaming = false
			m.runStartedAt = time.Time{}
		case port.OutputReasoning:
			m.appendReasoning(o.Content)
		case port.OutputProgress:
			m.messages = append(m.messages, chatMessage{kind: msgProgress, content: o.Content})
		case port.OutputToolStart:
			meta := cloneMessageMeta(o.Meta)
			if _, ok := meta["started_at"]; !ok {
				meta["started_at"] = m.now().UTC()
			}
			m.messages = append(m.messages, chatMessage{kind: msgToolStart, content: o.Content, meta: meta})
			m.recordProgressDetail("running", "tools", "starting "+summarizeTimelineToolStart(strings.TrimSpace(o.Content), toolMetaString(chatMessage{meta: meta}, "args_preview", "")), m.now().UTC())
		case port.OutputToolResult:
			m.markToolStartCompleted(o.Meta)
			meta := cloneMessageMeta(o.Meta)
			meta["completed_at"] = m.now().UTC()
			toolName := strings.TrimSpace(toolMetaString(chatMessage{meta: meta}, "tool", o.Content))
			isErr, _ := o.Meta["is_error"].(bool)
			if isErr {
				m.messages = append(m.messages, chatMessage{kind: msgToolError, content: o.Content, meta: meta})
				message := "error " + toolPrettyName(toolName)
				if detail := summarizeTimelineToolResult(toolName, o.Content); strings.TrimSpace(detail) != "" {
					message += " · " + detail
				}
				m.recordProgressDetail("running", "tools", message, m.now().UTC())
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgToolResult, content: o.Content, meta: meta})
				detail := summarizeTimelineToolResult(toolName, o.Content)
				message := "completed " + toolPrettyName(toolName)
				if strings.TrimSpace(detail) != "" {
					message += " · " + detail
				}
				m.recordProgressDetail("running", "tools", message, m.now().UTC())
			}
		}
		m.refreshViewport()
	}

	if msg.ask != nil {
		if resp, notice, ok := m.autoApproveAsk(msg.ask); ok {
			msg.ask.replyCh <- resp
			if strings.TrimSpace(notice) != "" {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: notice})
			}
			m.refreshViewport()
			return m, nil
		}
		m.pendAsk = msg.ask
		m.askForm = newAskFormState(msg.ask.request, m.workspace)
		m.openAskOverlay()
		notice := "Interactive input requested. Use Tab to navigate and Enter to confirm."
		if msg.ask.request.Type == port.InputConfirm && msg.ask.request.Approval != nil {
			notice = "Approval required. Review the requested action and choose how to proceed."
			m.recordProgressDetail("waiting", "approval", summarizeTimelineApproval(msg.ask.request.Approval), m.now().UTC())
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: notice})
		m.activateAskField()
		m.refreshViewport()
	}

	return m, nil
}

func (m *chatModel) appendStream(delta string) {
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].kind == msgAssistant && m.streaming {
		m.messages[len(m.messages)-1].content += delta
	} else {
		m.messages = append(m.messages, chatMessage{
			kind:    msgAssistant,
			content: delta,
			meta:    map[string]any{"timestamp": m.now().UTC()},
		})
		m.streaming = true
		if m.runStartedAt.IsZero() {
			m.runStartedAt = m.now().UTC()
		}
	}
}

func (m *chatModel) appendReasoning(delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].kind == msgReasoning {
		m.messages[len(m.messages)-1].content += delta
		return
	}
	m.messages = append(m.messages, chatMessage{
		kind:    msgReasoning,
		content: delta,
	})
}

func (m *chatModel) refreshViewport() {
	m.syncViewportLayout()
	content := renderAllMessages(m.messages, m.mainWidth(), m.toolCollapsed)
	if banner := m.renderStartupBanner(); banner != "" {
		content = banner + "\n\n" + content
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m chatModel) renderStartupBanner() string {
	if strings.TrimSpace(m.startupBanner) == "" {
		return ""
	}
	for _, msg := range m.messages {
		if msg.kind != msgSystem {
			return ""
		}
	}
	return titleStyle.Render(strings.TrimRight(m.startupBanner, "\r\n"))
}

func (m *chatModel) autoApproveAsk(ask *bridgeAsk) (port.InputResponse, string, bool) {
	if ask == nil || ask.request.Type != port.InputConfirm || ask.request.Approval == nil {
		return port.InputResponse{}, "", false
	}
	sessionID := approvalSessionID(ask.request.Approval, m.currentSessionID)
	for _, rule := range m.approvalRules[sessionID] {
		if !rule.matches(ask.request.Approval, m.currentSessionID) {
			continue
		}
		resp := port.InputResponse{
			Approved: true,
			Decision: &port.ApprovalDecision{
				RequestID: ask.request.Approval.ID,
				Approved:  true,
				Reason:    "remembered approval for this session",
				Source:    "tui-session-rule-auto",
				DecidedAt: m.now().UTC(),
			},
		}
		notice := "Approved automatically for this session"
		if strings.TrimSpace(rule.Label) != "" {
			notice += ": " + rule.Label
		}
		notice += "."
		return resp, notice, true
	}
	for _, rule := range m.projectApprovalRules {
		if !rule.matches(ask.request.Approval, m.currentSessionID) {
			continue
		}
		resp := port.InputResponse{
			Approved: true,
			Decision: &port.ApprovalDecision{
				RequestID: ask.request.Approval.ID,
				Approved:  true,
				Reason:    "remembered approval for this project",
				Source:    "tui-project-rule-auto",
				DecidedAt: m.now().UTC(),
			},
		}
		notice := "Approved automatically for this project"
		if strings.TrimSpace(rule.Label) != "" {
			notice += ": " + rule.Label
		}
		notice += "."
		return resp, notice, true
	}
	return port.InputResponse{}, "", false
}

func (m *chatModel) recalcLayout() {
	m.syncViewportLayout()
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

func (m *chatModel) syncViewportLayout() {
	inputWidth := m.inputWrapWidth()
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.textarea.SetWidth(inputWidth)
	m.adjustInputHeight()
	layout := m.generateLayout()

	if !m.ready {
		m.viewport = viewport.New(layout.MainWidth, layout.ViewportHeight)
		m.ready = true
		return
	}
	m.viewport.Width = layout.MainWidth
	m.viewport.Height = layout.ViewportHeight
}

func (m chatModel) visibleInputHeight() int {
	return m.editorPaneHeight(m.mainWidth())
}

func (m chatModel) visibleProgressHeight() int {
	return 0
}

func (m *chatModel) inputWrapWidth() int {
	width := m.mainWidth() - inputBorderStyle.GetHorizontalFrameSize()
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
	width := m.width
	if side := m.shellSidebarWidth(); side > 0 {
		width -= side + m.shellMainGapWidth()
	}
	if width < 40 {
		return 40
	}
	return width
}

func (m chatModel) displayApprovalMode() string {
	if strings.TrimSpace(m.approvalMode) == "" {
		return "(default)"
	}
	return m.approvalMode
}

func (m chatModel) compactPostureSummary() string {
	tokens := make([]string, 0, 4)
	tokens = append(tokens, valueOrDefaultString(strings.TrimSpace(m.profile), "default"))
	if trust := strings.TrimSpace(m.trust); trust != "" {
		tokens = append(tokens, trust)
	}
	if approval := strings.TrimSpace(m.approvalMode); approval != "" {
		tokens = append(tokens, approval)
	}
	if m.fastMode {
		tokens = append(tokens, "fast")
	}
	return strings.Join(tokens, " · ")
}

func (m chatModel) queueProfileSwitch(profileName, displayText string) (chatModel, tea.Cmd) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "profile name cannot be empty"})
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching profile to %s...", profileName)})
	m.streaming = true
	m.refreshViewport()
	return m, func() tea.Msg { return switchProfileMsg{profile: profileName, displayText: displayText} }
}

func (m chatModel) cycleProfile() (chatModel, tea.Cmd) {
	if m.streaming || m.hasRunningToolCalls() {
		return m, nil
	}
	current := valueOrDefaultString(strings.TrimSpace(m.profile), "default")
	names, err := runtime.ProfileNamesForWorkspace(m.workspace, m.trust)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list profiles: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	if len(names) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "no profiles available"})
		m.refreshViewport()
		return m, nil
	}
	next := names[0]
	for i, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), current) {
			next = names[(i+1)%len(names)]
			break
		}
	}
	return m.queueProfileSwitch(next, "")
}

func titleCaseWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 1 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[:1]) + s[1:]
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

// handleConfigCommand 处理 /config 命令。
func (m chatModel) handleConfigCommand(args []string) (chatModel, tea.Cmd) {
	if _, err := product.ConfigPath(); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Unable to determine config directory."})
		m.refreshViewport()
		return m, nil
	}

	// /config — 显示当前配置
	if len(args) == 0 {
		info, err := product.ShowConfig(nil, true)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Failed to load config: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		}
		m.refreshViewport()
		return m, nil
	}

	// /config set <key> <value>
	if args[0] == "set" && len(args) >= 3 {
		key := strings.ToLower(args[1])
		value := strings.Join(args[2:], " ")

		display, err := product.SetConfig(key, value, true)
		if err != nil {
			m.messages = append(m.messages, chatMessage{
				kind:    msgError,
				content: fmt.Sprintf("Failed to save config: %v", err),
			})
		} else {
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

func valueOrDefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
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
	appDir := config.AppDir()
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

func (m *chatModel) refreshSlashHints() {
	text := strings.TrimSpace(m.textarea.Value())
	m.slashHints = filterSlashHints(text, m.customCommands, m.discoveredSkills)
	m.slashPopup = buildSlashPopup(text, m.customCommands, m.discoveredSkills)
	// 保留 popup 中已选中的光标位置（输入变化时归零）
	if m.slashPopup != nil {
		m.slashPopup.cursor = 0
	}
}

func (m chatModel) currentSlashHints() []string {
	if m.streaming || m.pendAsk != nil {
		return nil
	}
	return m.slashHints
}

func (m *chatModel) applySlashCompletion() bool {
	// 如果弹窗可见，使用弹窗中选中的命令
	if m.slashPopup != nil && len(m.slashPopup.items) > 0 {
		selected := m.slashPopup.selected()
		m.textarea.SetValue(selected.name + " ")
		m.slashPopup = nil
		m.refreshSlashHints()
		return true
	}
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

type slashCommandDef struct {
	Name        string
	Summary     string
	Section     string
	HiddenInNav bool
}

var slashCommandCatalog = []slashCommandDef{
	{Name: "/help", Summary: "Show command help", Section: "Threads and core"},
	{Name: "/new", Summary: "Start a fresh thread", Section: "Threads and core"},
	{Name: "/status", Summary: "Show the current runtime and thread summary", Section: "Threads and core"},
	{Name: "/resume", Summary: "List or resume saved threads", Section: "Threads and core"},
	{Name: "/fork", Summary: "Branch into a fresh thread", Section: "Threads and core"},
	{Name: "/agent", Summary: "Inspect or cancel background agent threads", Section: "Threads and core"},
	{Name: "/ps", Summary: "Show background activity", Section: "Threads and core"},
	{Name: "/trace", Summary: "Inspect the last run trace", Section: "Threads and core"},
	{Name: "/clear", Summary: "Clear the visible transcript", Section: "Threads and core"},
	{Name: "/copy", Summary: "Copy the latest completed output", Section: "Threads and core"},
	{Name: "/compact", Summary: "Compact transcript and persist snapshot", Section: "Threads and core"},
	{Name: "/plan", Summary: "Switch to planning mode", Section: "Runtime posture"},
	{Name: "/model", Summary: "Show or switch the active model", Section: "Runtime posture"},
	{Name: "/models", Summary: "Open the configured model picker", Section: "Runtime posture"},
	{Name: "/fast", Summary: "Toggle fast interaction mode", Section: "Runtime posture"},
	{Name: "/personality", Summary: "Set the response personality", Section: "Runtime posture"},
	{Name: "/permissions", Summary: "Inspect or override runtime permissions", Section: "Runtime posture"},
	{Name: "/trust", Summary: "Switch trust posture", Section: "Runtime posture"},
	{Name: "/approval", Summary: "Switch approval posture", Section: "Runtime posture"},
	{Name: "/profile", Summary: "Show or switch profile", Section: "Runtime posture"},
	{Name: "/theme", Summary: "Show or switch the TUI theme", Section: "Runtime posture"},
	{Name: "/statusline", Summary: "Configure footer status items", Section: "Runtime posture"},
	{Name: "/experimental", Summary: "Toggle experimental product features", Section: "Runtime posture"},
	{Name: "/diff", Summary: "Show the current git diff", Section: "Review and recovery"},
	{Name: "/review", Summary: "Review working tree state", Section: "Review and recovery"},
	{Name: "/inspect", Summary: "Inspect runs, threads, replay, and governance", Section: "Review and recovery"},
	{Name: "/changes", Summary: "Inspect persisted change operations", Section: "Review and recovery"},
	{Name: "/apply", Summary: "Apply an explicit patch file", Section: "Review and recovery"},
	{Name: "/rollback", Summary: "Roll back a persisted change", Section: "Review and recovery"},
	{Name: "/checkpoint", Summary: "List, inspect, create, or replay checkpoints", Section: "Review and recovery"},
	{Name: "/git", Summary: "Run common git and gh helpers", Section: "Review and recovery"},
	{Name: "/init", Summary: "Scaffold AGENTS.md and custom commands", Section: "Tools and integrations"},
	{Name: "/mention", Summary: "Insert an @file mention into the composer", Section: "Tools and integrations"},
	{Name: "/search", Summary: "Search the web via Jina", Section: "Tools and integrations"},
	{Name: "/open", Summary: "Open a file in the local editor", Section: "Tools and integrations"},
	{Name: "/image", Summary: "Open or save the latest generated image", Section: "Tools and integrations"},
	{Name: "/media", Summary: "Attach, open, or save media", Section: "Tools and integrations"},
	{Name: "/mcp", Summary: "Inspect configured MCP servers", Section: "Tools and integrations"},
	{Name: "/schedules", Summary: "Browse scheduled jobs", Section: "Tools and integrations"},
	{Name: "/config", Summary: "Show or update config values", Section: "Tools and integrations"},
	{Name: "/debug", Summary: "Toggle local prompt preview in debug output", Section: "Tools and integrations"},
	{Name: "/debug-config", Summary: "Show config, paths, and logs", Section: "Tools and integrations"},
	{Name: "/skills", Summary: "List discovered skills", Section: "Tools and integrations"},
	{Name: "/skill", Summary: "Invoke a named skill", Section: "Tools and integrations"},
	{Name: "/http_request", Summary: "Invoke a tool shortcut directly", Section: "Tools and integrations"},
	{Name: "/exit", Summary: "Exit mosscode", Section: "Threads and core"},
	{Name: "/quit", Summary: "Exit mosscode", Section: "Threads and core", HiddenInNav: true},
}

func filterSlashHints(input string, customCommands []product.CustomCommand, discoveredSkills []string) []string {
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	if strings.Contains(input, " ") {
		return nil
	}
	lower := strings.ToLower(input)
	hints := make([]string, 0, 8)
	seen := make(map[string]struct{}, 16)
	for _, cmd := range slashCommandCatalog {
		if cmd.HiddenInNav {
			continue
		}
		if strings.HasPrefix(cmd.Name, lower) {
			if _, ok := seen[cmd.Name]; ok {
				continue
			}
			seen[cmd.Name] = struct{}{}
			hints = append(hints, cmd.Name)
		}
	}
	for _, cmd := range customCommands {
		name := "/" + cmd.Name
		if strings.HasPrefix(name, lower) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			hints = append(hints, name)
		}
	}
	for _, skillName := range discoveredSkills {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(skillName), "/")
		if name == "/" {
			continue
		}
		if strings.HasPrefix(name, lower) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			hints = append(hints, name)
		}
	}
	if len(hints) == 0 {
		return nil
	}
	sort.Strings(hints)
	if len(hints) > 8 {
		hints = hints[:8]
	}
	return hints
}

func (m chatModel) renderSlashHelp() string {
	sections := []string{"Threads and core", "Runtime posture", "Review and recovery", "Tools and integrations"}
	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, section := range sections {
		first := true
		for _, cmd := range slashCommandCatalog {
			if cmd.HiddenInNav || cmd.Section != section {
				continue
			}
			if first {
				b.WriteString("\n")
				b.WriteString(section)
				b.WriteString(":\n")
				first = false
			}
			fmt.Fprintf(&b, "  %-14s %s\n", cmd.Name, cmd.Summary)
		}
	}
	if len(m.customCommands) > 0 {
		b.WriteString("\nCustom commands:\n")
		for _, cmd := range m.customCommands {
			fmt.Fprintf(&b, "  %-14s %s\n", "/"+cmd.Name, cmd.Summary)
		}
	}
	b.WriteString("\nDirect skill/tool usage:\n")
	b.WriteString("  /skill <name> <task...>\n")
	b.WriteString("  /<skill_or_tool_name> <task...>\n")
	b.WriteString("\nCheckpoint details:\n")
	b.WriteString("  /checkpoint list [limit]\n")
	b.WriteString("  /checkpoint show <id|latest>\n")
	b.WriteString("  /checkpoint create [note]\n")
	b.WriteString("  /checkpoint replay [<id|latest>] [resume|rerun] [restore]\n")
	b.WriteString("\nKeyboard shortcuts:\n")
	b.WriteString("  double Esc     Cancel current running generation/tool execution\n")
	b.WriteString("  Ctrl+C         Clear input (press twice quickly to quit)\n")
	b.WriteString("  Ctrl+O         Collapse/expand tool messages\n")
	b.WriteString("  Enter (running) Queue message to run after current task\n")
	b.WriteString("  Up/Down        Navigate persisted input history\n")
	b.WriteString("  Shift+Enter    Insert newline\n")
	return strings.TrimRight(b.String(), "\n")
}

func (m chatModel) renderStatusSummary() string {
	var b strings.Builder
	b.WriteString("Runtime status:\n")
	fmt.Fprintf(&b, "Provider: %s\n", valueOrDefaultString(m.provider, "(default)"))
	fmt.Fprintf(&b, "Model: %s\n", valueOrDefaultString(m.model, "(default)"))
	fmt.Fprintf(&b, "Workspace: %s\n", valueOrDefaultString(m.workspace, "."))
	fmt.Fprintf(&b, "Run state: %s\n", valueOrDefaultRunState(m.streaming))
	fmt.Fprintf(&b, "Trust: %s\n", valueOrDefaultString(m.trust, "(unknown)"))
	fmt.Fprintf(&b, "Profile: %s\n", valueOrDefaultString(m.profile, "default"))
	fmt.Fprintf(&b, "Theme: %s\n", valueOrDefaultString(m.theme, themeDefault))
	fmt.Fprintf(&b, "Approval: %s\n", valueOrDefaultString(m.approvalMode, "(default)"))
	fmt.Fprintf(&b, "Personality: %s\n", valueOrDefaultString(m.personality, product.PersonalityFriendly))
	fmt.Fprintf(&b, "Fast mode: %t\n", m.fastMode)
	fmt.Fprintf(&b, "Status line: %s\n", strings.Join(normalizeStatusLineItems(m.statusLineItems), ", "))
	if m.progress.visible() {
		fmt.Fprintf(&b, "Progress: %s / %s\n", valueOrDefaultString(m.progress.Status, "(idle)"), valueOrDefaultString(m.progress.Phase, "(none)"))
	}
	if len(m.pendingAttachments) > 0 {
		fmt.Fprintf(&b, "Pending attachments: %d\n", len(m.pendingAttachments))
	}
	fmt.Fprintf(&b, "Experimental: %s", strings.Join(effectiveExperimentalFeatures(m.experimentalFeatures), ", "))
	if m.sessionInfoFn != nil {
		info := strings.TrimSpace(m.sessionInfoFn())
		if info != "" {
			b.WriteString("\n\n")
			b.WriteString(info)
		}
	}
	return b.String()
}

func (m chatModel) latestCopiableContent() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		switch m.messages[i].kind {
		case msgAssistant, msgSystem, msgToolResult, msgToolError:
			if text := strings.TrimSpace(m.messages[i].content); text != "" {
				return text
			}
		}
	}
	return ""
}

func boolPtr(v bool) *bool {
	return &v
}

func (m *chatModel) syncCustomCommands() string {
	commands, err := product.DiscoverCustomCommands(m.workspace, config.AppName(), m.trust)
	if err != nil {
		m.customCommands = nil
		m.refreshSlashHints()
		return fmt.Sprintf("warning: custom command discovery failed: %v", err)
	}
	reserved := make(map[string]struct{}, len(slashCommandCatalog))
	for _, cmd := range slashCommandCatalog {
		reserved[strings.TrimPrefix(cmd.Name, "/")] = struct{}{}
	}
	filtered := make([]product.CustomCommand, 0, len(commands))
	skipped := 0
	for _, cmd := range commands {
		if _, exists := reserved[cmd.Name]; exists {
			skipped++
			continue
		}
		filtered = append(filtered, cmd)
	}
	m.customCommands = filtered
	m.refreshSlashHints()
	if skipped > 0 {
		return fmt.Sprintf("warning: skipped %d custom commands because their names conflict with built-in commands", skipped)
	}
	return ""
}

func (m *chatModel) setDiscoveredSkills(names []string) {
	if len(names) == 0 {
		m.discoveredSkills = nil
		m.refreshSlashHints()
		return
	}
	sorted := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized := strings.TrimPrefix(strings.TrimSpace(name), "/")
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		sorted = append(sorted, normalized)
	}
	sort.Strings(sorted)
	m.discoveredSkills = sorted
	m.refreshSlashHints()
}

func (m chatModel) findCustomCommand(name string) (product.CustomCommand, bool) {
	target := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "/")
	for _, cmd := range m.customCommands {
		if cmd.Name == target {
			return cmd, true
		}
	}
	return product.CustomCommand{}, false
}

func (m chatModel) invokeSkillLikeCommand(name, task, displayText string) (chatModel, tea.Cmd) {
	if name == "" || task == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /skill <name> <task...>"})
		m.refreshViewport()
		return m, nil
	}
	prompt := fmt.Sprintf("Use skill or tool '%s' to complete this request:\n%s", name, task)
	return m.dispatchUserSubmission(displayText, prompt, []port.ContentPart{port.TextPart(prompt)})
}

func (m chatModel) dispatchUserSubmission(displayText, runText string, parts []port.ContentPart) (chatModel, tea.Cmd) {
	if len(parts) == 0 {
		parts = []port.ContentPart{port.TextPart(strings.TrimSpace(runText))}
	}
	if m.streaming {
		m.queuedInputs = append(m.queuedInputs, runText)
		m.queuedParts = append(m.queuedParts, parts)
		m.pendingAttachments = nil
		m.textarea.Reset()
		m.adjustInputHeight()
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{
		kind:    msgUser,
		content: strings.TrimSpace(displayText),
		meta:    map[string]any{"timestamp": m.now().UTC()},
	})
	m.pendingAttachments = nil
	m.textarea.Reset()
	m.adjustInputHeight()
	m.adjustInputHeight()
	m.streaming = true
	m.runStartedAt = m.now().UTC()
	m.refreshViewport()
	if m.sendFn != nil {
		m.sendFn(runText, parts)
	}
	return m, nil
}

func uiTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return uiTickMsg{} })
}

// uiTickIdleCmd 在非流式状态下使用低频心跳（节省 CPU）。
func uiTickIdleCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return uiTickMsg{} })
}

// isRunning 返回 true 当前有正在执行的 agent turn（streaming 或 tools 正在运行）。
func (m chatModel) isRunning() bool {
	return m.streaming || m.hasRunningToolCalls()
}

func cloneMessageMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func (m *chatModel) markToolStartCompleted(resultMeta map[string]any) {
	callID, _ := resultMeta["call_id"].(string)
	toolName, _ := resultMeta["tool"].(string)
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.kind != msgToolStart || msg.meta == nil {
			continue
		}
		if _, done := msg.meta["completed_at"]; done {
			continue
		}
		if callID != "" {
			if startID, _ := msg.meta["call_id"].(string); startID == callID {
				msg.meta["completed_at"] = m.now().UTC()
				return
			}
			continue
		}
		if toolName != "" {
			if startTool, _ := msg.meta["tool"].(string); startTool == toolName {
				msg.meta["completed_at"] = m.now().UTC()
				return
			}
		}
	}
}

func (m chatModel) hasRunningToolCalls() bool {
	for _, msg := range m.messages {
		if msg.kind != msgToolStart {
			continue
		}
		if msg.meta == nil {
			return true
		}
		if _, done := msg.meta["completed_at"]; !done {
			return true
		}
	}
	return false
}

// valueOrDefault 返回 s 或 defaultVal。
func valueOrDefault(s, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}
