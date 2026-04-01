package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	trace        *product.RunTraceSummary
	traceSummary string
	err          error
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
	sendFn                func(string)  // 发送用户消息给 agent
	cancelRunFn           func() bool   // 取消当前运行中的任务
	skillListFn           func() string // 查询已加载 skills
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
	refreshSystemPromptFn func() error
	pendAsk               *bridgeAsk // 当前阻塞的 Ask 请求
	askForm               *askFormState
	scheduleBrowser       *scheduleBrowserState
	finished              bool   // session 已结束
	result                string // 最终结果
	lastTrace             *product.RunTraceSummary
	currentSessionID      string
	progress              executionProgressState
	approvalRules         map[string][]approvalMemoryRule

	// 工具输出折叠
	toolCollapsed bool // true 时折叠 tool start/result 消息

	// 配置显示
	provider             string
	model                string
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

	queuedInputs []string

	inputHistory  []string
	historyCursor int
	historyDraft  string
	historyPath   string
	slashHints    []string

	now       func() time.Time
	lastEscAt time.Time
	lastCtrlC time.Time
	runStartedAt time.Time
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
	applyTheme(theme)

	return chatModel{
		textarea:             ta,
		provider:             provider,
		model:                model,
		workspace:            workspace,
		trust:                "trusted",
		theme:                theme,
		personality:          personality,
		fastMode:             fastMode,
		statusLineItems:      statusLineItems,
		experimentalFeatures: experimentalFeatures,
		toolCollapsed:        true,
		approvalRules:        map[string][]approvalMemoryRule{},
		inputHistory:         loadInputHistory(defaultHistoryPath(), maxInputHistory),
		historyPath:          defaultHistoryPath(),
		now:                  time.Now,
	}
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, uiTickCmd())
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
		if m.scheduleBrowser != nil {
			switch msg.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "esc":
				m.scheduleBrowser = nil
				m.refreshViewport()
				return m, nil
			}
			return m.handleScheduleBrowserKey(msg)
		}
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

	case tea.MouseMsg:
		switch msg.String() {
		case "wheel up":
			m.viewport.LineUp(3)
			return m, nil
		case "wheel down":
			m.viewport.LineDown(3)
			return m, nil
		}

	case bridgeMsg:
		return m.handleBridge(msg)

	case refreshMsg:
		m.refreshViewport()
		return m, nil

	case uiTickMsg:
		if m.streaming || m.hasRunningToolCalls() {
			m.refreshViewport()
		}
		return m, uiTickCmd()

	case notificationProgressMsg:
		if msg.SetCurrent && strings.TrimSpace(msg.Snapshot.SessionID) != "" {
			m.currentSessionID = strings.TrimSpace(msg.Snapshot.SessionID)
			m.progress = msg.Snapshot
			m.refreshViewport()
			return m, nil
		}
		if strings.TrimSpace(msg.Snapshot.SessionID) == "" || (m.currentSessionID != "" && strings.TrimSpace(msg.Snapshot.SessionID) != m.currentSessionID) {
			return m, nil
		}
		m.progress = msg.Snapshot
		m.refreshViewport()
		return m, nil

	case sessionResultMsg:
		m.streaming = false
		m.finished = true
		m.runStartedAt = time.Time{}
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.err.Error()})
		}
		if msg.trace != nil {
			traceCopy := *msg.trace
			m.lastTrace = &traceCopy
		}
		if strings.TrimSpace(msg.traceSummary) != "" {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: msg.traceSummary})
		}
		if msg.output != "" {
			m.result = msg.output
		}
		if len(m.queuedInputs) > 0 && m.sendFn != nil {
			next := m.queuedInputs[0]
			m.queuedInputs = m.queuedInputs[1:]
			m.streaming = true
			m.finished = false
			m.runStartedAt = m.now().UTC()
			m.sendFn(next)
		}
		m.refreshViewport()
		m.textarea.Focus()
		return m, nil

	case threadSwitchResultMsg:
		m.streaming = false
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.err.Error()})
			m.refreshViewport()
			return m, nil
		}
		m.messages = []chatMessage{{kind: msgSystem, content: msg.output}}
		m.finished = false
		m.result = ""
		m.lastTrace = nil
		m.queuedInputs = nil
		m.textarea.Reset()
		m.adjustInputHeight()
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
	runText, err := expandInlineFileMentions(text, m.workspace)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to attach mentioned files: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	return m.dispatchUserSubmission(text, runText)
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
		case port.OutputProgress:
			m.messages = append(m.messages, chatMessage{kind: msgProgress, content: o.Content})
		case port.OutputToolStart:
			meta := cloneMessageMeta(o.Meta)
			if _, ok := meta["started_at"]; !ok {
				meta["started_at"] = m.now().UTC()
			}
			m.messages = append(m.messages, chatMessage{kind: msgToolStart, content: o.Content, meta: meta})
		case port.OutputToolResult:
			m.markToolStartCompleted(o.Meta)
			meta := cloneMessageMeta(o.Meta)
			meta["completed_at"] = m.now().UTC()
			isErr, _ := o.Meta["is_error"].(bool)
			if isErr {
				m.messages = append(m.messages, chatMessage{kind: msgToolError, content: o.Content, meta: meta})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgToolResult, content: o.Content, meta: meta})
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
		m.askForm = newAskFormState(msg.ask.request)
		notice := "Interactive input requested. Use Tab to navigate and Enter to confirm."
		if msg.ask.request.Type == port.InputConfirm && msg.ask.request.Approval != nil {
			notice = "Approval required. Review the requested action and choose how to proceed."
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

func (m *chatModel) refreshViewport() {
	m.syncViewportLayout()
	content := renderAllMessages(m.messages, m.mainWidth(), m.toolCollapsed)
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
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
				Reason:    "remembered approval for this thread",
				Source:    "tui-thread-rule-auto",
				DecidedAt: m.now().UTC(),
			},
		}
		notice := "Approved automatically for this thread"
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
	inputWidth := m.width - 4
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.textarea.SetWidth(inputWidth)
	m.adjustInputHeight()

	headerH := 2 // 顶栏
	metaH := 1 + m.visibleProgressHeight()
	gapH := 1 // 消息区/输入区、输入区/状态栏之间的空行
	inputH := m.visibleInputHeight()
	statusH := 1

	vpHeight := m.height - headerH - metaH - gapH - inputH - statusH
	if vpHeight < 3 {
		vpHeight = 3
	}

	if !m.ready {
		m.viewport = viewport.New(m.mainWidth(), vpHeight)
		m.ready = true
		return
	}
	m.viewport.Width = m.mainWidth()
	m.viewport.Height = vpHeight
}

func (m chatModel) visibleInputHeight() int {
	extra := 0
	if len(m.queuedInputs) > 0 {
		extra++
	}
	switch {
	case m.pendAsk != nil && m.askForm != nil:
		return extra + lipgloss.Height(m.renderAskForm(m.mainWidth()-2))
	case m.scheduleBrowser != nil:
		return extra + lipgloss.Height(m.renderScheduleBrowser(m.mainWidth()-2))
	default:
		height := extra + m.inputBoxHeight()
		if m.streaming {
			height++
		}
		height++
		return height
	}
}

func (m chatModel) visibleProgressHeight() int {
	if m.progress.renderLine(m.now(), m.mainWidth()) == "" {
		return 0
	}
	return 1
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

func (m chatModel) displayApprovalMode() string {
	if strings.TrimSpace(m.approvalMode) == "" {
		return "(default)"
	}
	return m.approvalMode
}

func (m chatModel) compactPostureSummary() string {
	tokens := make([]string, 0, 4)
	if profile := strings.TrimSpace(m.profile); profile != "" && !strings.EqualFold(profile, "default") {
		tokens = append(tokens, profile)
	}
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

func (m chatModel) renderHeaderMetaLine() string {
	parts := []string{titleCaseWord(valueOrDefaultRunState(m.streaming))}
	if threadID := strings.TrimSpace(m.currentSessionID); threadID != "" {
		parts = append(parts, "thread "+threadID)
	}
	if posture := m.compactPostureSummary(); posture != "" {
		parts = append(parts, posture)
	}
	return strings.Join(parts, "  │  ")
}

func (m chatModel) renderSlashHintLine() string {
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		return mutedStyle.Render("  /help for commands  │  Tab completes slash commands")
	}
	return mutedStyle.Render("  Suggestions: " + strings.Join(hints, "  │  ") + "  (Tab to complete)")
}

func (m chatModel) renderFooterHelpLine() string {
	toolHint := "Ctrl+O expand tools"
	if !m.toolCollapsed {
		toolHint = "Ctrl+O collapse tools"
	}
	base := fmt.Sprintf("/help  │  %s  │  ↑↓ history  │  Esc Esc cancel  │  Ctrl+C clear/quit", toolHint)
	status := strings.TrimSpace(m.renderStatusLine())
	if status == "" {
		return truncateDisplayWidth(base, m.mainWidth())
	}
	return truncateDisplayWidth(base+"  │  "+status, m.mainWidth())
}

func (m chatModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var b strings.Builder

	// 顶栏
	header := titleStyle.Render("mosscode")
	infoText := strings.TrimSpace(m.provider)
	if m.model != "" {
		infoText += " (" + strings.TrimSpace(m.model) + ")"
	}
	if ws := valueOrDefaultString(m.workspace, "."); ws != "." && ws != "" {
		infoText += " · " + filepath.Base(ws)
	}
	info := statusBarStyle.Render("  " + infoText)
	b.WriteString(topBarStyle.Render(header + info))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// 输入框上方元信息（精简）
	b.WriteString(mutedStyle.Render(m.renderHeaderMetaLine()))
	b.WriteString("\n")
	if progressLine := m.progress.renderLine(m.now(), m.mainWidth()); progressLine != "" {
		b.WriteString(progressLine)
		b.WriteString("\n")
	}

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
	} else if m.scheduleBrowser != nil {
		b.WriteString(m.renderScheduleBrowser(m.mainWidth() - 2))
	} else {
		if m.streaming {
			b.WriteString(runningStyle.Render(fmt.Sprintf(
				"  %s Running (%s, double Esc to cancel current run)",
				spinnerFrame(m.now()),
				formatElapsed(m.runStartedAt, m.now()),
			)))
			b.WriteString("\n")
		}
		b.WriteString(m.renderSlashHintLine())
		b.WriteString("\n")
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// 底部状态
	status := mutedStyle.Render(m.renderStatusLine())
	if m.pendAsk != nil && m.askForm != nil {
		if m.pendAsk.request.Type == port.InputConfirm && m.pendAsk.request.Approval != nil {
			status = mutedStyle.Render("Tab/Shift+Tab move focus │ ↑↓ choose decision │ Enter apply │ approval memory applies to this thread only")
		} else {
			status = mutedStyle.Render("Tab/Shift+Tab move fields │ ↑↓ choose options │ Space toggle multi-select │ Enter confirm")
		}
	} else if m.scheduleBrowser != nil {
		status = mutedStyle.Render("↑↓ choose schedule │ e run now │ d delete │ r refresh │ Esc close")
	} else if m.pendAsk != nil {
		status = mutedStyle.Render("Type your reply and press Enter │ double Esc cancel run │ Ctrl+C clear input")
	} else {
		status = mutedStyle.Render(m.renderFooterHelpLine())
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
	draft := strings.TrimSpace(m.textarea.Value())

	m.textarea.Reset()

	switch cmd {
	case "/exit", "/quit":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Goodbye"})
		m.refreshViewport()
		return m, func() tea.Msg { return cancelMsg{} }

	case "/model":
		if len(args) == 0 {
			currentModel := m.model
			if strings.TrimSpace(currentModel) == "" {
				currentModel = "(default)"
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("Current model: %s\nProvider: %s\nUsage: /model <model>", currentModel, m.provider),
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

	case "/fast":
		if len(args) == 0 || strings.EqualFold(args[0], "status") {
			state := "off"
			if m.fastMode {
				state = "on"
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("Fast mode: %s\nUsage: /fast <on|off|status>", state),
			})
			m.refreshViewport()
			return m, nil
		}
		var enabled bool
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "on":
			enabled = true
		case "off":
			enabled = false
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fast <on|off|status>"})
			m.refreshViewport()
			return m, nil
		}
		if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.FastMode = boolPtr(enabled)
			return nil
		}); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update fast mode: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.fastMode = enabled
		if m.refreshSystemPromptFn != nil {
			if err := m.refreshSystemPromptFn(); err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("fast mode saved but failed to refresh the current thread prompt: %v", err)})
				m.refreshViewport()
				return m, nil
			}
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Fast mode %s. New turns in the current thread will use the updated interaction mode.", onOff(enabled))})
		m.refreshViewport()
		return m, nil

	case "/personality":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{
				kind:    msgSystem,
				content: fmt.Sprintf("Current personality: %s\nUsage: /personality <friendly|pragmatic|none>", valueOrDefaultString(m.personality, product.PersonalityFriendly)),
			})
			m.refreshViewport()
			return m, nil
		}
		personality := product.NormalizePersonality(args[0])
		if personality == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /personality <friendly|pragmatic|none>"})
			m.refreshViewport()
			return m, nil
		}
		if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.Personality = personality
			return nil
		}); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update personality: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.personality = personality
		if m.refreshSystemPromptFn != nil {
			if err := m.refreshSystemPromptFn(); err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("personality saved but failed to refresh the current thread prompt: %v", err)})
				m.refreshViewport()
				return m, nil
			}
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Personality set to %s for this product surface and the current thread.", personality)})
		m.refreshViewport()
		return m, nil

	case "/clear":
		m.messages = nil
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: "Conversation cleared.",
		})
		m.refreshViewport()
		return m, nil

	case "/copy":
		content := m.latestCopiableContent()
		if strings.TrimSpace(content) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "No completed output is available to copy yet."})
			m.refreshViewport()
			return m, nil
		}
		if err := writeClipboard(content); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to copy output: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Copied the latest completed output to the clipboard."})
		}
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
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Current thread summary moved to /status. Use /resume to continue saved threads."})
		m.refreshViewport()
		return m, nil

	case "/status":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.renderStatusSummary()})
		m.refreshViewport()
		return m, nil

	case "/resume":
		if len(args) == 0 {
			if m.sessionListFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Saved-thread listing is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			out, err := m.sessionListFn(20)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list resumable threads: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		if len(args) > 1 || m.sessionRestoreFn == nil {
			if m.sessionRestoreFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Resume is unavailable."})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /resume [session_id|latest]"})
			}
			m.refreshViewport()
			return m, nil
		}
		id := strings.TrimSpace(args[0])
		if id == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /resume [session_id|latest]"})
			m.refreshViewport()
			return m, nil
		}
		return m.startThreadSwitch(fmt.Sprintf("Resuming thread %s...", id), func() (string, error) {
			out, err := m.sessionRestoreFn(id)
			if err != nil {
				return "", fmt.Errorf("failed to resume thread: %v", err)
			}
			return out, nil
		})

	case "/trace":
		limit := 20
		if len(args) >= 1 {
			v, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /trace [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		if m.lastTrace == nil {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "No run trace is available yet. Run a task first."})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderRunTraceDetail(*m.lastTrace, limit)})
		m.refreshViewport()
		return m, nil

	case "/new":
		if m.newSessionFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "New thread creation is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		return m.startThreadSwitch("Starting a fresh thread...", func() (string, error) {
			out, err := m.newSessionFn()
			if err != nil {
				return "", fmt.Errorf("failed to create new thread: %v", err)
			}
			return out, nil
		})

	case "/checkpoint":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /checkpoint list [limit]\n  /checkpoint show <checkpoint_id|latest>\n  /checkpoint create [note]\n  /checkpoint replay [<checkpoint_id|latest>] [resume|rerun] [restore]"})
			m.refreshViewport()
			return m, nil
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			if m.checkpointListFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint list is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			limit := 20
			if len(args) >= 2 {
				v, err := strconv.Atoi(args[1])
				if err != nil || v <= 0 {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint list [limit:int]"})
					m.refreshViewport()
					return m, nil
				}
				limit = v
			}
			out, err := m.checkpointListFn(limit)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list checkpoints: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		case "show":
			if m.checkpointShowFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint detail is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint show <checkpoint_id|latest>"})
				m.refreshViewport()
				return m, nil
			}
			out, err := m.checkpointShowFn(strings.TrimSpace(args[1]))
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show checkpoint: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		case "create":
			if m.checkpointCreateFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint creation is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			note := strings.TrimSpace(strings.Join(args[1:], " "))
			out, err := m.checkpointCreateFn(note)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to create checkpoint: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		case "replay":
			if m.checkpointReplayFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint replay is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			checkpointID := ""
			mode := ""
			restore := false
			rest := args[1:]
			if len(rest) > 0 {
				first := strings.ToLower(strings.TrimSpace(rest[0]))
				switch first {
				case "", "resume", "rerun", "restore", "--restore-worktree":
				default:
					checkpointID = strings.TrimSpace(rest[0])
					rest = rest[1:]
				}
			}
			for _, item := range rest {
				token := strings.ToLower(strings.TrimSpace(item))
				switch token {
				case "resume", "rerun":
					mode = token
				case "restore", "--restore-worktree":
					restore = true
				default:
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint replay [<checkpoint_id|latest>] [resume|rerun] [restore]"})
					m.refreshViewport()
					return m, nil
				}
			}
			label := "latest"
			if strings.TrimSpace(checkpointID) != "" {
				label = checkpointID
			}
			return m.startThreadSwitch(fmt.Sprintf("Replaying checkpoint %s...", label), func() (string, error) {
				out, err := m.checkpointReplayFn(checkpointID, mode, restore)
				if err != nil {
					return "", fmt.Errorf("failed to replay checkpoint: %v", err)
				}
				return out, nil
			})
		case "fork":
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint branching moved to /fork. Use /fork [session <id>|checkpoint <id|latest>|latest] [restore]."})
			m.refreshViewport()
			return m, nil
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint list|show|create|replay ..."})
			m.refreshViewport()
			return m, nil
		}

	case "/fork":
		if m.checkpointForkFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Fork is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		sourceKind := string(port.ForkSourceSession)
		sourceID := ""
		restore := false
		rest := args
		if len(rest) > 0 {
			switch strings.ToLower(strings.TrimSpace(rest[0])) {
			case "session", "checkpoint":
				sourceKind = strings.ToLower(strings.TrimSpace(rest[0]))
				if sourceKind == string(port.ForkSourceSession) && (len(rest) < 2 || strings.TrimSpace(rest[1]) == "") {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fork [session <id>|checkpoint <id|latest>|latest] [restore]"})
					m.refreshViewport()
					return m, nil
				}
				if len(rest) >= 2 && strings.TrimSpace(rest[1]) != "" {
					sourceID = strings.TrimSpace(rest[1])
					rest = rest[2:]
				} else {
					rest = rest[1:]
				}
			case "latest":
				sourceKind = string(port.ForkSourceCheckpoint)
				rest = rest[1:]
			}
		}
		for _, item := range rest {
			token := strings.ToLower(strings.TrimSpace(item))
			if token == "restore" || token == "--restore-worktree" {
				restore = true
				continue
			}
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fork [session <id>|checkpoint <id|latest>|latest] [restore]"})
			m.refreshViewport()
			return m, nil
		}
		label := sourceKind
		if strings.TrimSpace(sourceID) != "" {
			label += " " + sourceID
		}
		return m.startThreadSwitch(fmt.Sprintf("Forking from %s...", strings.TrimSpace(label)), func() (string, error) {
			out, err := m.checkpointForkFn(sourceKind, sourceID, restore)
			if err != nil {
				return "", fmt.Errorf("failed to fork thread: %v", err)
			}
			return out, nil
		})

	case "/changes":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /changes list [limit]\n  /changes show <change_id>"})
			m.refreshViewport()
			return m, nil
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			if m.changeListFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change list is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			limit := 20
			if len(args) >= 2 {
				v, err := strconv.Atoi(args[1])
				if err != nil || v <= 0 {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes list [limit:int]"})
					m.refreshViewport()
					return m, nil
				}
				limit = v
			}
			out, err := m.changeListFn(limit)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list changes: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		case "show":
			if m.changeShowFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change detail is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes show <change_id>"})
				m.refreshViewport()
				return m, nil
			}
			out, err := m.changeShowFn(strings.TrimSpace(args[1]))
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show change: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes list|show ..."})
			m.refreshViewport()
			return m, nil
		}

	case "/apply":
		if m.applyChangeFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change apply is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /apply <patch_file> [summary...]"})
			m.refreshViewport()
			return m, nil
		}
		patchFile := strings.TrimSpace(args[0])
		summary := strings.TrimSpace(strings.Join(args[1:], " "))
		out, err := m.applyChangeFn(patchFile, summary)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to apply change: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/rollback":
		if m.rollbackChangeFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change rollback is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /rollback <change_id>"})
			m.refreshViewport()
			return m, nil
		}
		out, err := m.rollbackChangeFn(strings.TrimSpace(args[0]))
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to roll back change: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/diff":
		if m.gitRunFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Diff inspection is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		cmdArgs := []string{"--no-pager", "diff"}
		if len(args) > 0 {
			cmdArgs = append(cmdArgs, "--")
			cmdArgs = append(cmdArgs, args...)
		}
		out, err := m.gitRunFn("git", cmdArgs)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git diff failed: %v", err)})
		} else if strings.TrimSpace(out) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "No diff."})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/review":
		report, err := product.BuildReviewReport(context.Background(), m.workspace, args)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("review failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderReviewReport(report)})
		}
		m.refreshViewport()
		return m, nil

	case "/sessions":
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Saved-session browsing moved to /resume. Use /resume to list recent sessions or /resume <session_id> to continue one."})
		m.refreshViewport()
		return m, nil

	case "/mcp":
		if len(args) == 0 || strings.EqualFold(args[0], "list") {
			servers, err := product.ListMCPServers(m.workspace, m.trust)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list MCP servers: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderMCPServerList(servers)})
			}
			m.refreshViewport()
			return m, nil
		}
		if strings.EqualFold(args[0], "show") && len(args) == 2 {
			servers, err := product.GetMCPServer(m.workspace, m.trust, args[1])
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show MCP server: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderMCPServerDetail(servers)})
			}
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /mcp\n  /mcp list\n  /mcp show <name>"})
		m.refreshViewport()
		return m, nil

	case "/compact":
		if m.offloadFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Context compaction is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		keepRecent := 20
		note := "manual compact from TUI"
		if len(args) >= 1 {
			v, err := strconv.Atoi(args[0])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /compact [keep_recent:int] [note...]"})
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
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("compact failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/offload":
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Transcript compaction moved to /compact. Use /compact [keep_recent] [note]."})
		m.refreshViewport()
		return m, nil

	case "/agent":
		if m.taskListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread controls are unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 || args[0] == "list" {
			listArgs := args
			if len(listArgs) > 0 && listArgs[0] == "list" {
				listArgs = listArgs[1:]
			}
			status := ""
			limit := 20
			if len(listArgs) >= 1 {
				status = strings.TrimSpace(strings.ToLower(listArgs[0]))
				switch status {
				case "", "running", "completed", "failed", "cancelled":
				default:
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent [list] [running|completed|failed|cancelled] [limit]"})
					m.refreshViewport()
					return m, nil
				}
			}
			if len(listArgs) >= 2 {
				v, err := strconv.Atoi(listArgs[1])
				if err != nil || v <= 0 {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent [list] [status] [limit:int]"})
					m.refreshViewport()
					return m, nil
				}
				limit = v
			}
			out, err := m.taskListFn(status, limit)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list agent threads: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		if args[0] == "current" {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.renderStatusSummary()})
			m.refreshViewport()
			return m, nil
		}
		if args[0] == "cancel" {
			if m.taskCancelFn == nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread cancellation is unavailable."})
				m.refreshViewport()
				return m, nil
			}
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent cancel <agent_id> [reason...]"})
				m.refreshViewport()
				return m, nil
			}
			agentID := strings.TrimSpace(args[1])
			reason := "cancelled by user from TUI"
			if len(args) >= 3 {
				reason = strings.Join(args[2:], " ")
			}
			out, err := m.taskCancelFn(agentID, reason)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to cancel agent thread: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
			m.refreshViewport()
			return m, nil
		}
		if m.taskQueryFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread query is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		agentID := strings.TrimSpace(args[0])
		out, err := m.taskQueryFn(agentID)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to query agent thread: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/ps":
		if !m.experimentalEnabled(product.ExperimentalBackgroundPS) {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background activity view is disabled. Use /experimental enable background-ps to turn it back on."})
			m.refreshViewport()
			return m, nil
		}
		if m.taskListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background activity view is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		status := "running"
		limit := 10
		if len(args) >= 1 {
			status = strings.ToLower(strings.TrimSpace(args[0]))
			switch status {
			case "running", "completed", "failed", "cancelled", "all":
			default:
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /ps [running|completed|failed|cancelled|all] [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			if status == "all" {
				status = ""
			}
		}
		if len(args) >= 2 {
			v, err := strconv.Atoi(strings.TrimSpace(args[1]))
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /ps [running|completed|failed|cancelled|all] [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.taskListFn(status, limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to inspect background activity: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/tasks":
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background agent controls moved to /agent. Use /agent [list], /agent current, /agent <id>, or /agent cancel <id>."})
		m.refreshViewport()
		return m, nil

	case "/task":
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background agent controls moved to /agent. Use /agent <id> or /agent cancel <id> [reason]."})
		m.refreshViewport()
		return m, nil

	case "/config":
		return m.handleConfigCommand(args)

	case "/schedules":
		if m.scheduleCtrl == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Schedule listing is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		items, err := m.scheduleCtrl.List()
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list schedules: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		if len(items) > 0 {
			m.scheduleBrowser = newScheduleBrowserState(items)
			m.refreshViewport()
			return m, nil
		}
		out, err := m.scheduleCtrl.ListText()
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list schedules: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

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
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Budget summary moved into /status."})
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

	case "/approval":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current approval mode: %s\nUsage: /approval <read-only|confirm|full-auto>", m.displayApprovalMode())})
			m.refreshViewport()
			return m, nil
		}
		nextMode := product.NormalizeApprovalMode(args[0])
		if err := product.ValidateApprovalMode(nextMode); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching approval mode to %s...", nextMode)})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg { return switchApprovalMsg{mode: nextMode} }

	case "/profile":
		if len(args) == 0 {
			current := valueOrDefaultString(m.profile, "default")
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current profile: %s\nUsage: /profile list\n       /profile set <name>", current)})
			m.refreshViewport()
			return m, nil
		}
		if strings.EqualFold(args[0], "list") {
			names, err := runtime.ProfileNamesForWorkspace(m.workspace, m.trust)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list profiles: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Available profiles:\n- %s", strings.Join(names, "\n- "))})
			}
			m.refreshViewport()
			return m, nil
		}
		profileName := strings.TrimSpace(args[0])
		if strings.EqualFold(args[0], "set") {
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /profile set <name>"})
				m.refreshViewport()
				return m, nil
			}
			profileName = strings.TrimSpace(args[1])
		}
		if profileName == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "profile name cannot be empty"})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching profile to %s...", profileName)})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg { return switchProfileMsg{profile: profileName} }

	case "/plan":
		prompt := strings.TrimSpace(strings.Join(args, " "))
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Switching to planning mode..."})
		m.streaming = true
		m.refreshViewport()
		return m, func() tea.Msg {
			return switchProfileMsg{
				profile:     "planning",
				prompt:      prompt,
				displayText: input,
			}
		}

	case "/help":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.renderSlashHelp()})
		m.refreshViewport()
		return m, nil

	case "/debug-config":
		if m.debugConfigFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Debug config view is unavailable."})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.debugConfigFn()})
		}
		m.refreshViewport()
		return m, nil

	case "/theme":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current theme: %s", m.theme)})
			m.refreshViewport()
			return m, nil
		}
		raw := strings.ToLower(strings.TrimSpace(args[0]))
		if raw != themeDefault && raw != themePlain {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /theme [default|plain]"})
			m.refreshViewport()
			return m, nil
		}
		m.theme = raw
		applyTheme(raw)
		if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.Theme = raw
			return nil
		}); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("theme switched locally but failed to persist: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switched theme to %s and saved it to config.", raw)})
		m.refreshViewport()
		return m, nil

	case "/statusline":
		if !m.experimentalEnabled(product.ExperimentalStatuslineCustomization) {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Status-line customization is disabled. Use /experimental enable statusline-customization to turn it back on."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: renderStatusLineUsage(m.statusLineItems)})
			m.refreshViewport()
			return m, nil
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "reset":
			m.statusLineItems = append([]string(nil), defaultStatusLineItems...)
			if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
				cfg.StatusLine = nil
				return nil
			}); err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to reset status line: %v", err)})
				m.refreshViewport()
				return m, nil
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Status line reset to the default footer items."})
			m.refreshViewport()
			return m, nil
		case "set":
			if len(args) < 2 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: renderStatusLineUsage(m.statusLineItems)})
				m.refreshViewport()
				return m, nil
			}
			items, err := parseStatusLineItems(strings.Join(args[1:], " "))
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
				m.refreshViewport()
				return m, nil
			}
			m.statusLineItems = items
			if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
				cfg.StatusLine = append([]string(nil), items...)
				return nil
			}); err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update status line: %v", err)})
				m.refreshViewport()
				return m, nil
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Status line updated: %s", strings.Join(items, ", "))})
			m.refreshViewport()
			return m, nil
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: renderStatusLineUsage(m.statusLineItems)})
			m.refreshViewport()
			return m, nil
		}

	case "/experimental":
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderExperimentalFeatures(config.TUIConfig{Experimental: m.experimentalFeatures})})
			m.refreshViewport()
			return m, nil
		}
		if len(args) != 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /experimental [enable|disable] <feature>"})
			m.refreshViewport()
			return m, nil
		}
		action := strings.ToLower(strings.TrimSpace(args[0]))
		name := product.NormalizeExperimentalFeature(args[1])
		if name == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("unknown experimental feature %q", args[1])})
			m.refreshViewport()
			return m, nil
		}
		enabled := action == "enable"
		if action != "enable" && action != "disable" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /experimental [enable|disable] <feature>"})
			m.refreshViewport()
			return m, nil
		}
		cfg, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.Experimental = setExperimentalFeature(cfg.Experimental, name, enabled)
			return nil
		})
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update experimental features: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.experimentalFeatures = effectiveExperimentalFeatures(cfg.Experimental)
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Experimental feature %s %s.", name, onOff(enabled))})
		m.refreshViewport()
		return m, nil

	case "/search":
		query := strings.TrimSpace(strings.Join(args, " "))
		if query == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /search <query>"})
			m.refreshViewport()
			return m, nil
		}
		return m.invokeSkillLikeCommand("jina_search", query, input)

	case "/open":
		target := strings.TrimSpace(strings.Join(args, " "))
		if target == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /open <path[:line]>"})
			m.refreshViewport()
			return m, nil
		}
		out, err := openWorkspacePath(m.workspace, target)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("open failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	case "/mention":
		if !m.experimentalEnabled(product.ExperimentalComposerMentions) {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Composer mentions are disabled. Use /experimental enable composer-mentions to turn them back on."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) == 0 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /mention <path>\nTip: the command inserts @path into the composer so the next send attaches that file."})
			m.refreshViewport()
			return m, nil
		}
		token, err := mentionTokenForComposer(m.workspace, strings.Join(args, " "))
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
			m.refreshViewport()
			return m, nil
		}
		nextDraft := strings.TrimSpace(strings.TrimPrefix(draft, input))
		nextDraft = strings.TrimSpace(strings.Join([]string{nextDraft, token}, " "))
		m.textarea.SetValue(nextDraft)
		m.adjustInputHeight()
		m.refreshSlashHints()
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Inserted %s into the composer. Send the next turn to attach it.", token)})
		m.refreshViewport()
		return m, nil

	case "/init":
		out, err := product.InitWorkspaceBootstrap(m.workspace, config.AppName())
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("init failed: %v", err)})
		} else {
			if notice := m.syncCustomCommands(); notice != "" {
				out += "\n" + notice
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil

	default:
		if custom, ok := m.findCustomCommand(cmd); ok {
			runText := product.RenderCustomCommandPrompt(custom, strings.TrimSpace(strings.Join(args, " ")), m.workspace)
			if runText == "" {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("custom command /%s is empty", custom.Name)})
				m.refreshViewport()
				return m, nil
			}
			return m.dispatchUserSubmission(input, runText)
		}
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

func truncateForQueue(s string, max int) string {
	if max < 12 {
		max = 12
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func truncateDisplayWidth(s string, max int) string {
	if max < 8 {
		max = 8
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	if max <= 3 {
		return strings.Repeat(".", max)
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if width+rw > max-3 {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	b.WriteString("...")
	return b.String()
}

func (m *chatModel) refreshSlashHints() {
	text := strings.TrimSpace(m.textarea.Value())
	m.slashHints = filterSlashHints(text, m.customCommands, m.discoveredSkills)
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
	{Name: "/changes", Summary: "Inspect persisted change operations", Section: "Review and recovery"},
	{Name: "/apply", Summary: "Apply an explicit patch file", Section: "Review and recovery"},
	{Name: "/rollback", Summary: "Roll back a persisted change", Section: "Review and recovery"},
	{Name: "/checkpoint", Summary: "List, inspect, create, or replay checkpoints", Section: "Review and recovery"},
	{Name: "/git", Summary: "Run common git and gh helpers", Section: "Review and recovery"},
	{Name: "/init", Summary: "Scaffold AGENTS.md and custom commands", Section: "Tools and integrations"},
	{Name: "/mention", Summary: "Insert an @file mention into the composer", Section: "Tools and integrations"},
	{Name: "/search", Summary: "Search the web via Jina", Section: "Tools and integrations"},
	{Name: "/open", Summary: "Open a file in the local editor", Section: "Tools and integrations"},
	{Name: "/mcp", Summary: "Inspect configured MCP servers", Section: "Tools and integrations"},
	{Name: "/schedules", Summary: "Browse scheduled jobs", Section: "Tools and integrations"},
	{Name: "/config", Summary: "Show or update config values", Section: "Tools and integrations"},
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
	m.messages = append(m.messages, chatMessage{
		kind:    msgUser,
		content: strings.TrimSpace(displayText),
		meta:    map[string]any{"timestamp": m.now().UTC()},
	})
	m.textarea.Reset()
	m.adjustInputHeight()
	m.adjustInputHeight()
	m.streaming = true
	m.runStartedAt = m.now().UTC()
	m.refreshViewport()
	if m.sendFn != nil {
		m.sendFn(runText)
	}
	return m, nil
}

func uiTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return uiTickMsg{} })
}

func spinnerFrame(now time.Time) string {
	frames := []string{"-", "\\", "|", "/"}
	if now.IsZero() {
		return frames[0]
	}
	idx := (now.UnixNano() / int64(200*time.Millisecond)) % int64(len(frames))
	return frames[idx]
}

func formatElapsed(start, now time.Time) string {
	if start.IsZero() {
		return "0.0s"
	}
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := now.Sub(start)
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := elapsed.Seconds()
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
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
