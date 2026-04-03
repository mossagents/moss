package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/kernel/port"
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
		case "shift+tab":
			return m.cycleProfile()
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
		for _, part := range msg.outputImages {
			if part.Type != port.ContentPartOutputImage {
				continue
			}
			path := strings.TrimSpace(part.SourcePath)
			if path == "" {
				path = strings.TrimSpace(part.URL)
			}
			if path == "" {
				path = "(inline image)"
			}
			m.messages = append(m.messages, chatMessage{
				kind:    msgAssistant,
				content: fmt.Sprintf("Generated image: %s", path),
				meta: map[string]any{
					"timestamp":         m.now().UTC(),
					"is_image":          true,
					"image_path":        path,
					"image_url":         strings.TrimSpace(part.URL),
					"image_source_path": strings.TrimSpace(part.SourcePath),
					"image_mime_type":   strings.TrimSpace(part.MIMEType),
					"image_data_base64": strings.TrimSpace(part.DataBase64),
				},
			})
		}
		if len(m.queuedInputs) > 0 && m.sendFn != nil {
			next := m.queuedInputs[0]
			m.queuedInputs = m.queuedInputs[1:]
			nextParts := []port.ContentPart{port.TextPart(next)}
			if len(m.queuedParts) > 0 {
				nextParts = m.queuedParts[0]
				m.queuedParts = m.queuedParts[1:]
			}
			m.streaming = true
			m.finished = false
			m.runStartedAt = m.now().UTC()
			m.sendFn(next, nextParts)
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
		m.queuedParts = nil
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
