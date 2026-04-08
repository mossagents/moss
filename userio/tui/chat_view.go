package tui

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"strings"
	"time"
)

func (m chatModel) renderHeaderMetaLine() string {
	parts := []string{halfMutedStyle.Render(titleCaseWord(valueOrDefaultRunState(m.streaming)))}
	if threadID := strings.TrimSpace(m.currentSessionID); threadID != "" {
		parts = append(parts, shellHeaderDetailStyle.Render("thread "+threadID))
	}
	if posture := m.compactPostureSummary(); posture != "" {
		parts = append(parts, shellHeaderDetailStyle.Render(posture))
	}
	return strings.Join(parts, shellHeaderSeparatorStyle.Render(" • "))
}

func (m chatModel) renderSlashHintLine() string {
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		return composerHintStyle.Render("  /help commands  •  @ files  •  Tab completes slash commands")
	}
	return composerHintStyle.Render("  Suggestions: " + strings.Join(hints, "  •  ") + "  (Tab to complete)")
}

func (m chatModel) renderFooterHelpLine() string {
	toolHint := "Ctrl+O expand tools"
	if !m.toolCollapsed {
		toolHint = "Ctrl+O collapse tools"
	}
	base := fmt.Sprintf("/help  •  %s  •  Shift+Tab next profile  •  ↑↓ history  •  Esc Esc cancel  •  Ctrl+C clear/quit", toolHint)
	return truncateDisplayWidth(base, m.mainWidth())
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
