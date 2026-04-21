package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	userattachments "github.com/mossagents/moss/harness/userio/attachments"
	"github.com/mossagents/moss/kernel/model"
)

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		return m, nil

	case tea.KeyMsg:
		if overlay := m.activeOverlay(); overlay != nil {
			switch msg.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			}
			return overlay.HandleKey(m, msg)
		}
		// インラインセレクト（InputSelect）のキー処理：オーバーレイなしで独自に処理
		if m.isInlineSelectAsk() {
			switch msg.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "esc":
				if m.cancelRunFn != nil && m.cancelRunFn() {
					m.streaming = false
					m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Input cancelled."})
				}
				m.resetAskFormState()
				m.refreshViewport()
				return m, nil
			default:
				return m.handleAskKey(msg)
			}
		}
		// インライン承認/確認フォーム（InputConfirm）のキー処理：オーバーレイなしで独自に処理
		if m.isApprovalAskActive() || m.isSimpleConfirmAskActive() {
			switch msg.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "esc":
				if m.cancelRunFn != nil && m.cancelRunFn() {
					m.streaming = false
					m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Input cancelled."})
				}
				m.resetAskFormState()
				m.refreshViewport()
				return m, nil
			default:
				return m.handleAskKey(msg)
			}
		}
		// Extension key bindings evaluated before built-in handlers.
		ctx := m.tuiContext()
		for _, ext := range m.extensions {
			if handler, ok := ext.KeyBindings[msg.String()]; ok {
				if consumed, cmd := handler(ctx); consumed {
					return m, cmd
				}
			}
		}
		switch msg.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "esc":
			// 弹窗可见时，Esc 关闭弹窗，不传递给上层
			if m.slashPopup != nil {
				m.slashPopup = nil
				m.refreshViewport()
				return m, nil
			}
			if m.mentionPopup != nil {
				m.mentionPopup = nil
				m.refreshViewport()
				return m, nil
			}
			if m.skillsPopup != nil {
				m.skillsPopup = nil
				m.refreshViewport()
				return m, nil
			}
			return m.handleEsc()
		case "ctrl+t":
			if m.transcriptOverlay != nil {
				return m.closeTranscriptOverlay(), nil
			}
			m.openTranscriptOverlay()
			m = m.initTranscriptOverlay()
			return m, nil
		case "ctrl+o":
			m.toolCollapsed = !m.toolCollapsed
			m.refreshViewport()
			return m, nil
		case "ctrl+y":
			hasCopyable := false
			for _, msg := range m.messages {
				if isCopyableMessage(msg) {
					hasCopyable = true
					break
				}
			}
			if !hasCopyable {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "No completed output is available to copy yet."})
				m.refreshViewport()
				return m, nil
			}
			m.copyPicker = newCopyPickerState(m.messages)
			m.openCopyOverlay()
			return m, nil
		case "ctrl+v":
			text, err := readClipboard()
			if err == nil && strings.TrimSpace(text) != "" {
				m.textarea.InsertString(text)
				m.adjustInputHeight()
				m.historyCursor = len(m.inputHistory)
				m.historyDraft = m.textarea.Value()
				m.historySlashSuppressed = false
				m.historyMentionSuppressed = false
				m.refreshSlashHints()
				m.refreshMentionPopup()
			}
			return m, nil
		case "ctrl+x":
			if len(m.pendingAttachments) > 0 {
				m.pendingAttachments = append([]userattachments.ComposerAttachment(nil), m.pendingAttachments[:len(m.pendingAttachments)-1]...)
			}
			return m, nil
		case "up", "down":
			delta := -1
			if msg.String() == "down" {
				delta = 1
			}
			// Popups: navigate list items.
			if m.slashPopup != nil {
				m.slashPopup.move(delta)
				return m, nil
			}
			if m.mentionPopup != nil {
				m.mentionPopup.move(delta)
				return m, nil
			}
			if m.skillsPopup != nil {
				m.skillsPopup.move(delta)
				return m, nil
			}
			if hints := m.currentSlashHints(); len(hints) > 0 {
				return m, nil
			}
			// Navigate input history.
			dir := "up"
			if msg.String() == "down" {
				dir = "down"
			}
			return m.handleHistoryNavigation(dir)
		case " ":
			// 空格：skills 弹窗可见时切换选中项，否则正常插入
			if m.skillsPopup != nil {
				return m.handleSkillsToggle()
			}
		case "tab":
			if m.applySlashCompletion() {
				m.adjustInputHeight()
				return m, nil
			}
			if m.applyMentionCompletion() {
				m.adjustInputHeight()
				return m, nil
			}
			return m, nil
		case "shift+tab":
			return m.cycleMode()
		case "enter":
			// 弹窗可见时，Enter 确认选中项
			if m.slashPopup != nil {
				if m.applySlashCompletion() {
					m.adjustInputHeight()
					return m, nil
				}
			}
			if m.mentionPopup != nil {
				if m.applyMentionCompletion() {
					m.adjustInputHeight()
					return m, nil
				}
			}
			return m.handleSend()
		}

	case bridgeMsg:
		return m.handleBridge(msg)

	case refreshMsg:
		m.refreshViewport()
		return m, nil

	case uiTickMsg:
		if m.isRunning() {
			m.refreshViewport()
			return m, uiTickCmd() // 流式状态下保持 150ms 刷新（动画）
		}
		return m, uiTickIdleCmd() // 空闲时降至 5s 心跳（节省 CPU）

	case notificationProgressMsg:
		if msg.SetCurrent && strings.TrimSpace(msg.Snapshot.SessionID) != "" {
			reset := strings.TrimSpace(m.currentSessionID) != strings.TrimSpace(msg.Snapshot.SessionID)
			m.currentSessionID = strings.TrimSpace(msg.Snapshot.SessionID)
			m.applyProgressSnapshot(msg.Snapshot, reset)
			m.progress = msg.Snapshot
			m.refreshViewport()
			return m, nil
		}
		if strings.TrimSpace(msg.Snapshot.SessionID) == "" || (m.currentSessionID != "" && strings.TrimSpace(msg.Snapshot.SessionID) != m.currentSessionID) {
			return m, nil
		}
		m.applyProgressSnapshot(msg.Snapshot, false)
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
		for _, part := range msg.outputMedia {
			path := strings.TrimSpace(part.SourcePath)
			if path == "" {
				path = strings.TrimSpace(part.URL)
			}
			if path == "" {
				path = "(inline media)"
			}
			mediaKind := outputMediaKind(part.Type)
			if mediaKind == "" {
				continue
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgAssistant,
				content: fmt.Sprintf("Generated %s: %s", mediaKind, path),
				meta: map[string]any{
					"timestamp":          m.now().UTC(),
					"is_media":           true,
					"media_kind":         mediaKind,
					"media_path":         path,
					"media_url":          strings.TrimSpace(part.URL),
					"media_source_path":  strings.TrimSpace(part.SourcePath),
					"media_mime_type":    strings.TrimSpace(part.MIMEType),
					"media_data_base64":  strings.TrimSpace(part.DataBase64),
					"media_content_type": string(part.Type),
				},
			})
		}
		if len(m.queuedInputs) > 0 && m.sendFn != nil {
			next := m.queuedInputs[0]
			m.queuedInputs = m.queuedInputs[1:]
			nextParts := []model.ContentPart{model.TextPart(next)}
			if len(m.queuedParts) > 0 {
				nextParts = m.queuedParts[0]
				m.queuedParts = m.queuedParts[1:]
			}
			m.streaming = true
			m.finished = false
			m.activeRunSummary = summarizeActiveRun(next)
			m.runStartedAt = m.now().UTC()
			m.sendFn(next, nextParts)
		}
		m.textarea.Focus()
		m.refreshViewport()
		return m, nil

	case gitBranchMsg:
		m.gitBranch = string(msg)
		return m, nil

	case appendSystemMessageMsg:
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: msg.text})
		m.refreshViewport()
		return m, nil

	case appendErrorMessageMsg:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.text})
		m.refreshViewport()
		return m, nil

	case openCustomOverlayMsg:
		for _, ext := range m.extensions {
			if ext.Overlays != nil {
				if factory, ok := ext.Overlays[msg.id]; ok {
					m.ensureOverlayStack()
					m.overlays.Open(overlayExt, extOverlayAdapter{impl: factory()})
					return m, nil
				}
			}
		}
		return m, nil

	case closeCustomOverlayMsg:
		if m.overlays != nil {
			m.overlays.Close(overlayExt)
		}
		return m, nil

	case sendFromOverlayMsg:
		if m.overlays != nil {
			m.overlays.Close(overlayExt)
		}
		if strings.TrimSpace(msg.text) != "" {
			return m.submitInjectedText(msg.text)
		}
		return m, nil

	case threadSwitchResultMsg:
		m.streaming = false
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.err.Error()})
			m.refreshViewport()
			return m, nil
		}
		m.messages = []chatMessage{{kind: msgSystem, content: msg.output}}
		m.printedCount = 0 // 重置打印计数，新线程重新打印
		m.finished = false
		m.result = ""
		m.lastTrace = nil
		m.queuedInputs = nil
		m.queuedParts = nil
		m.textarea.Reset()
		m.adjustInputHeight()
		m.textarea.Focus()
		m.refreshViewport()
		return m, nil

	case bangResultMsg:
		m.streaming = false
		m.runStartedAt = time.Time{}
		m.cancelRunFn = nil
		if msg.output != "" {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: msg.output})
		} else if msg.err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: msg.err.Error()})
		}
		m.refreshViewport()
		return m, nil

	case skillToggleResultMsg:
		if msg.err != nil {
			// 回滚本地乐观更新
			if m.skillsPopup != nil {
				for i := range m.skillsPopup.items {
					if m.skillsPopup.items[i].name == msg.name {
						m.skillsPopup.items[i].enabled = !msg.enabled
						break
					}
				}
			}
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("切换 skill %q 失败：%v", msg.name, msg.err)})
		}
		m.refreshViewport()
		return m, nil
	}

	// 更新子组件
	if m.pendAsk == nil {
		var cmd tea.Cmd
		prevValue := m.textarea.Value()
		m.textarea, cmd = m.textarea.Update(msg)
		if m.textarea.Value() != prevValue {
			m.historySlashSuppressed = false
			m.historyMentionSuppressed = false
		}
		m.adjustInputHeight()
		m.historyCursor = len(m.inputHistory)
		m.historyDraft = m.textarea.Value()
		m.refreshSlashHints()
		m.refreshMentionPopup()
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func outputMediaKind(typ model.ContentPartType) string {
	switch typ {
	case model.ContentPartOutputImage:
		return "image"
	case model.ContentPartOutputAudio:
		return "audio"
	case model.ContentPartOutputVideo:
		return "video"
	default:
		return ""
	}
}

// handleSkillsToggle 处理 skills 弹窗中的空格键：切换选中项的启用状态。
// 本地状态立即翻转，实际激活/停用在后台执行。
func (m chatModel) handleSkillsToggle() (chatModel, tea.Cmd) {
	if m.skillsPopup == nil || len(m.skillsPopup.items) == 0 {
		return m, nil
	}
	item := m.skillsPopup.selected()
	newEnabled := !item.enabled
	m.skillsPopup.toggleCurrent() // 乐观更新本地状态
	m.refreshViewport()
	if m.skillToggleFn == nil {
		return m, nil
	}
	toggleFn := m.skillToggleFn
	name := item.name
	return m, func() tea.Msg {
		if err := toggleFn(name, newEnabled); err != nil {
			return skillToggleResultMsg{name: name, err: err}
		}
		return skillToggleResultMsg{name: name, enabled: newEnabled}
	}
}

type skillToggleResultMsg struct {
	name    string
	enabled bool
	err     error
}
