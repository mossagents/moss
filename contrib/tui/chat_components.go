package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	intr "github.com/mossagents/moss/kernel/interaction"
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

	var sections []string
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
	if progress := m.renderProgressBlock(layout.MainWidth); strings.TrimSpace(progress) != "" {
		sections = append(sections, progress)
	}
	if m.streaming {
		runLabel := shimmerText("Running", m.now())
		sections = append(sections, fmt.Sprintf("  %s %s (%s, double Esc to cancel current run)",
			spinnerFrame(m.now()),
			runLabel,
			formatElapsed(m.runStartedAt, m.now()),
		))
	}
	// 斜杠命令弹窗：替代普通 hint 行，提供可导航的富文本候选列表
	if m.slashPopup != nil && len(m.slashPopup.items) > 0 {
		sections = append(sections, m.renderSlashPopup(layout.MainWidth))
	} else if m.mentionPopup != nil && len(m.mentionPopup.items) > 0 {
		// @ 文件补全弹窗：inline 替代 overlay
		sections = append(sections, m.renderMentionPopup(layout.MainWidth))
	} else {
		sections = append(sections, m.renderSlashHintLine())
	}
	sections = append(sections, inputBorderStyle.Render(m.textarea.View()))
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
	width := min(88, max(52, layout.MainWidth-10))
	overlay := dialog.View(m, width, layout.BodyHeight)
	if strings.TrimSpace(overlay) == "" {
		return ""
	}
	return lipgloss.Place(layout.MainWidth, layout.BodyHeight, lipgloss.Center, lipgloss.Center, overlay)
}

func (m chatModel) renderStatusPane(width int) string {
	var status string
	if m.pendAsk != nil && m.askForm != nil {
		if m.pendAsk.request.Type == intr.InputConfirm && m.pendAsk.request.Approval != nil {
			status = "Tab/Shift+Tab move focus • ↑↓ choose decision • Enter apply • memory applies to this thread only"
		} else {
			status = "Tab/Shift+Tab move fields • ↑↓ choose options • Space toggle multi-select • Enter confirm"
		}
	} else if m.scheduleBrowser != nil {
		status = "↑↓ choose schedule • e run now • d delete • r refresh • Esc close"
	} else if m.pendAsk != nil {
		status = "Type your reply and press Enter • double Esc cancel run • Ctrl+C clear input"
	} else if len(m.pendingAttachments) > 0 {
		status = "Enter send • Ctrl+X remove latest attachment • Shift+Enter newline"
	} else {
		status = truncateDisplayWidth(m.renderStatusLine(), width)
	}
	return lipgloss.NewStyle().Width(width).Render(statusHintStyle.Render(status))
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
