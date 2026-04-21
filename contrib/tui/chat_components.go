package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/kernel/io"
)

// renderStreamingPreview 在流式输出期间，渲染当前流式消息的末尾预览行。
// 为用户提供实时反馈，同时不污染终端滚动区。
func (m chatModel) renderStreamingPreview(width int) string {
	if !m.streaming || len(m.messages) == 0 {
		return ""
	}
	// 找最后一条 assistant 或 reasoning 消息
	var content string
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.kind == msgAssistant || msg.kind == msgReasoning {
			content = msg.content
			break
		}
		// 遇到非流式消息就停止向前找
		if msg.kind != msgReasoning {
			break
		}
	}
	if strings.TrimSpace(content) == "" {
		return ""
	}

	// 截取最后 N 行作为预览
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	const maxPreviewLines = 5
	if len(lines) > maxPreviewLines {
		lines = lines[len(lines)-maxPreviewLines:]
	}

	// 用 muted 样式显示预览，最大宽度为 width
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(halfMutedStyle.Render(truncateDisplayWidth(line, width)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m chatModel) renderEditorPane(layout chatUILayout) string {
	if layout.EditorHeight <= 0 {
		return ""
	}

	// InputSelect タイプのインラインフォームはエディタ領域全体を置き換える
	if m.isInlineSelectAsk() {
		form := m.renderInlineSelectAsk(layout.MainWidth)
		return lipgloss.NewStyle().
			Width(layout.MainWidth).
			PaddingLeft(2).
			Render(form)
	}

	// InputConfirm タイプ（承認/シンプル確認）のインラインフォームはエディタ領域全体を置き換える
	if m.isApprovalAskActive() {
		form := m.renderInlineApprovalAsk(layout.MainWidth)
		return lipgloss.NewStyle().
			Width(layout.MainWidth).
			PaddingLeft(2).
			Render(form)
	}
	if m.isSimpleConfirmAskActive() {
		form := m.renderInlineSimpleConfirmAsk(layout.MainWidth)
		return lipgloss.NewStyle().
			Width(layout.MainWidth).
			PaddingLeft(2).
			Render(form)
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
	// 斜杠命令弹窗与 @ 文件补全弹窗移至输入框下方，先渲染输入框
	sections = append(sections, m.renderComposerInput(layout.MainWidth))
	if m.skillsPopup != nil {
		m.skillsPopup.height = layout.Height
		sections = append(sections, m.renderSkillsPopup(layout.MainWidth))
	} else if m.slashPopup != nil && len(m.slashPopup.items) > 0 {
		sections = append(sections, m.renderSlashPopup(layout.MainWidth))
	} else if m.mentionPopup != nil && len(m.mentionPopup.items) > 0 {
		// @ 文件补全弹窗：inline 替代 overlay
		sections = append(sections, m.renderMentionPopup(layout.MainWidth))
	}
	return lipgloss.NewStyle().
		Width(layout.MainWidth).
		Render(strings.Join(sections, "\n"))
}

// renderComposerInput 渲染带 ❯ 前缀指示符的输入区域，上下各一条水平分隔线。
// 第一行左侧显示 ❯，后续行用等宽空格对齐，整体宽度与 mainWidth 一致。
func (m chatModel) renderComposerInput(mainWidth int) string {
	raw := m.textarea.View()
	lines := strings.Split(raw, "\n")

	// 根据当前状态选择指示符颜色
	indStyle := lipgloss.NewStyle().Foreground(colorBorder)
	if m.slashPopup != nil || m.mentionPopup != nil ||
		strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/") ||
		strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "@") {
		indStyle = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	} else if m.pendAsk != nil {
		indStyle = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	} else if m.streaming {
		indStyle = lipgloss.NewStyle().Foreground(colorPrimary)
	} else if strings.TrimSpace(m.textarea.Value()) != "" || len(m.pendingAttachments) > 0 {
		indStyle = lipgloss.NewStyle().Foreground(colorSubtle)
	}

	ruleStr := strings.Repeat("─", mainWidth)
	rule := indStyle.Render(ruleStr)

	const indWidth = 2 // "❯ " 的宽度
	ind := indStyle.Render("❯ ")
	pad := strings.Repeat(" ", indWidth)

	rendered := make([]string, 0, len(lines)+2)
	rendered = append(rendered, rule)
	for i, line := range lines {
		if i == 0 {
			rendered = append(rendered, ind+line)
		} else {
			rendered = append(rendered, pad+line)
		}
	}
	rendered = append(rendered, rule)
	return strings.Join(rendered, "\n")
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
	width, vertical := m.overlayPlacement(dialog, layout)
	overlay := dialog.View(m, width, layout.BodyHeight)
	if strings.TrimSpace(overlay) == "" {
		return ""
	}
	return lipgloss.Place(layout.MainWidth, layout.BodyHeight, lipgloss.Center, vertical, overlay)
}

func (m chatModel) overlayPlacement(dialog overlayDialog, layout chatUILayout) (int, lipgloss.Position) {
	width := min(84, max(48, layout.MainWidth-12))
	vertical := lipgloss.Center
	switch dialog.ID() {
	case overlayAsk:
		if m.pendAsk != nil && m.pendAsk.request.Type == io.InputConfirm {
			width = min(layout.MainWidth, max(56, layout.MainWidth-2))
			vertical = lipgloss.Bottom
		}
	case overlayHelp, overlaySchedule, overlayStatus, overlayModel, overlayTheme, overlayMCP, overlayResume, overlayFork, overlayAgent, overlayMention, overlayCopy:
		vertical = lipgloss.Bottom
	}
	if width < 1 {
		width = 1
	}
	return width, vertical
}

func (m chatModel) renderStatusPane(width int) string {
	inner := max(1, width-statusBarStyle.GetHorizontalFrameSize())

	// Determine left hint text based on current state.
	var leftStr, rightStr string
	switch {
	case m.isApprovalAskActive():
		leftStr = approvalDecisionHelp(m.askForm.fields[0].def.Options)
		rightStr = "approval"
	case m.isSimpleConfirmAskActive():
		leftStr = simpleConfirmHelp()
		rightStr = "confirm"
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
		leftStr = "ctrl+y copy  •  ↑↓ history  •  /help"
		ctx := m.tuiContext()
		for _, ext := range m.extensions {
			for _, widget := range ext.StatusWidgets {
				if seg := strings.TrimSpace(widget(ctx)); seg != "" {
					leftStr += "  •  " + seg
				}
			}
		}
	}

	// Status line items on the right (unless overridden above).
	if rightStr == "" {
		rightStr = m.renderStatusLineBar()
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
