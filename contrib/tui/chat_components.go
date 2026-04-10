package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/kernel/io"
)

func (m chatModel) renderMainPane(layout chatUILayout) string {
	sections := []string{
		m.renderHeaderMetaLine(),
	}
	sections = append(sections, lipgloss.NewStyle().Height(layout.ViewportHeight).Render(m.viewport.View()))
	return lipgloss.NewStyle().
		Width(layout.MainWidth).
		Height(layout.MainHeight).
		Render(strings.Join(sections, "\n"))
}

func (m chatModel) renderEditorPane(layout chatUILayout) string {
	if layout.EditorHeight <= 0 {
		return ""
	}

	sections := []string{m.renderComposerMetaLine(layout.MainWidth)}
	if len(m.queuedInputs) > 0 {
		queueLines := make([]string, 0, len(m.queuedInputs)+1)
		queueLines = append(queueLines, fmt.Sprintf("Queued messages (%d)", len(m.queuedInputs)))
		for i, q := range m.queuedInputs {
			if i >= 5 {
				queueLines = append(queueLines, fmt.Sprintf("...and %d more", len(m.queuedInputs)-i))
				break
			}
			queueLines = append(queueLines, fmt.Sprintf("%d) %s", i+1, truncateForQueue(q, layout.MainWidth-12)))
		}
		sections = append(sections, composerHintStyle.Render("  "+strings.Join(queueLines, "  •  ")))
	}
	if len(m.pendingAttachments) > 0 {
		rows := make([]string, 0, len(m.pendingAttachments)+1)
		rows = append(rows, fmt.Sprintf("Attachments (%d)", len(m.pendingAttachments)))
		for i, item := range m.pendingAttachments {
			if i >= 5 {
				rows = append(rows, fmt.Sprintf("...and %d more", len(m.pendingAttachments)-i))
				break
			}
			rows = append(rows, fmt.Sprintf("[%s] %s", item.Kind, truncateForQueue(item.Label, layout.MainWidth-24)))
		}
		rows = append(rows, "Ctrl+X removes the latest attachment")
		sections = append(sections, composerHintStyle.Render("  "+strings.Join(rows, "  •  ")))
	}
	// 斜杠命令弹窗：替代普通 hint 行，提供可导航的富文本候选列表
	if m.slashPopup != nil && len(m.slashPopup.items) > 0 {
		sections = append(sections, m.renderSlashPopup(layout.MainWidth))
	} else if m.mentionPopup != nil && len(m.mentionPopup.items) > 0 {
		// @ 文件补全弹窗：inline 替代 overlay
		sections = append(sections, m.renderMentionPopup(layout.MainWidth))
	}
	boxStyle := composerBoxStyle.Copy()
	if draft := strings.TrimSpace(m.textarea.Value()); draft != "" || len(m.pendingAttachments) > 0 {
		boxStyle = boxStyle.BorderForeground(colorSubtle)
	}
	if m.slashPopup != nil || m.mentionPopup != nil || strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/") || strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "@") {
		boxStyle = boxStyle.BorderForeground(colorPrimary)
	}
	if m.pendAsk != nil {
		boxStyle = boxStyle.BorderForeground(colorSecondary)
	} else if m.streaming {
		boxStyle = boxStyle.BorderForeground(colorPrimary)
	}
	sections = append(sections, boxStyle.Render(m.textarea.View()))
	return lipgloss.NewStyle().
		Width(layout.MainWidth).
		Height(layout.EditorHeight).
		Render(strings.Join(sections, "\n"))
}

func (m chatModel) renderOverlayPane(layout chatUILayout) string {
	dialog := m.activeOverlay()
	if dialog == nil {
		return ""
	}
	// transcript overlay は全画面表示（センタリング不要）
	if dialog.ID() == overlayTranscript {
		return dialog.View(m, layout.MainWidth, layout.BodyHeight)
	}
	width := min(84, max(48, layout.MainWidth-12))
	overlay := dialog.View(m, width, layout.BodyHeight)
	if strings.TrimSpace(overlay) == "" {
		return ""
	}
	vertical := lipgloss.Center
	switch dialog.ID() {
	case overlayHelp, overlaySchedule, overlayStatus, overlayModel, overlayTheme, overlayMCP, overlayResume, overlayFork, overlayAgent, overlayMention, overlayCopy:
		vertical = lipgloss.Bottom
	}
	return lipgloss.Place(layout.MainWidth, layout.BodyHeight, lipgloss.Center, vertical, overlay)
}

func (m chatModel) renderStatusPane(width int) string {
	inner := max(1, width-statusBarStyle.GetHorizontalFrameSize())

	// Determine left hint text based on current state.
	var leftStr, rightStr string
	switch {
	case m.pendAsk != nil && m.askForm != nil && m.pendAsk.request.Type == io.InputConfirm && m.pendAsk.request.Approval != nil:
		leftStr = "Tab move  •  ↑↓ choose  •  Enter apply"
		rightStr = "approval"
	case m.scheduleBrowser != nil:
		leftStr = "↑↓ choose  •  e run  •  d delete  •  Esc close"
	case m.pendAsk != nil:
		leftStr = "Enter confirm  •  Esc Esc cancel"
	case m.hasActiveOverlay():
		leftStr = "↑↓ move  •  Enter confirm  •  Esc close"
	case m.mentionPopup != nil || m.slashPopup != nil:
		leftStr = "↑↓ move  •  Tab select  •  Esc close"
	case m.streaming:
		if !m.runStartedAt.IsZero() {
			leftStr = formatElapsed(m.runStartedAt, m.now()) + "  •  "
		}
		leftStr += "Esc Esc cancel"
	default:
		leftStr = "ctrl+y copy  •  alt+↑/↓ history  •  shift+tab profile  •  ctrl+o tools  •  /help"
		ctx := m.tuiContext()
		for _, ext := range m.extensions {
			for _, widget := range ext.StatusWidgets {
				if seg := strings.TrimSpace(widget(ctx)); seg != "" {
					leftStr += "  •  " + seg
				}
			}
		}
	}

	// Thread ID on the right (unless overridden above).
	if rightStr == "" {
		if threadID := strings.TrimSpace(m.currentSessionID); threadID != "" {
			rightStr = "thread " + shortThreadID(threadID)
		}
	}

	left := statusHintStyle.Render(leftStr)
	leftW := lipgloss.Width(left)

	if strings.TrimSpace(rightStr) == "" {
		if leftW > inner {
			left = statusHintStyle.Render(truncateDisplayWidth(leftStr, inner))
		}
		return statusBarStyle.Width(width).Render(left)
	}

	right := shellHeaderDetailStyle.Render(rightStr)
	rightW := lipgloss.Width(right)
	maxLeft := max(8, inner-rightW-1)
	if leftW > maxLeft {
		left = statusHintStyle.Render(truncateDisplayWidth(leftStr, maxLeft))
		leftW = lipgloss.Width(left)
	}
	gapW := max(0, inner-leftW-rightW)
	gap := strings.Repeat(" ", gapW)
	return statusBarStyle.Width(width).Render(left + gap + right)
}

func (m chatModel) renderBody(layout chatUILayout) string {
	mainBody := m.renderMainPane(layout)
	if overlay := m.renderOverlayPane(layout); strings.TrimSpace(overlay) != "" {
		mainBody = overlay
	} else if editor := m.renderEditorPane(layout); strings.TrimSpace(editor) != "" {
		mainBody = lipgloss.JoinVertical(lipgloss.Left, mainBody, editor)
	}
	return mainBody
}
