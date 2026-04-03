package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/mossagents/moss/kernel/port"
)

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
	base := fmt.Sprintf("/help  │  %s  │  Shift+Tab next profile  │  ↑↓ history  │  Esc Esc cancel  │  Ctrl+C clear/quit", toolHint)
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
