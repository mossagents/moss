package tui

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"strings"
	"time"
)

func (m chatModel) renderHeaderMetaLine() string {
	parts := []string{statusAccentStyle.Render(titleCaseWord(m.runtimeStateLabel()))}
	if threadID := strings.TrimSpace(m.currentSessionID); threadID != "" {
		parts = append(parts, shellHeaderDetailStyle.Render("thread "+shortThreadID(threadID)))
	}
	if posture := m.compactPostureSummary(); posture != "" {
		parts = append(parts, shellHeaderDetailStyle.Render(posture))
	}
	if m.streaming && !m.runStartedAt.IsZero() {
		parts = append(parts, shellHeaderDetailStyle.Render(formatElapsed(m.runStartedAt, m.now())))
	}
	return shellMetaBarStyle.Render(strings.Join(parts, shellHeaderSeparatorStyle.Render(" • ")))
}

func (m chatModel) renderSlashHintLine() string {
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		return composerHintStyle.Render("  / commands  •  @ files  •  Tab completes")
	}
	return composerHintStyle.Render("  Commands: " + strings.Join(hints, "  •  ") + "  •  Tab completes")
}

func (m chatModel) renderComposerMetaLine(width int) string {
	label, detail := m.composerMetaSummary()
	line := "  " + label
	if strings.TrimSpace(detail) != "" {
		line += "  •  " + detail
	}
	return composerHintStyle.Width(width).Render(truncateDisplayWidth(line, max(12, width)))
}

func (m chatModel) renderFooterHelpLine() string {
	if m.streaming {
		return ""
	}
	if m.mentionPopup != nil {
		return "Esc close"
	}
	if m.slashPopup != nil {
		return "Esc close"
	}
	if m.hasActiveOverlay() {
		return "↑↓ move  •  Enter confirm  •  Esc close"
	}
	return "/help"
}

func (m chatModel) View() string {
	if !m.ready {
		return "Loading..."
	}
	layout := m.generateLayout()
	body := m.renderBody(layout)

	return strings.Join([]string{
		m.renderShellHeader(),
		body,
		m.renderStatusPane(layout.Width),
	}, "\n")
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

func shortThreadID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}
