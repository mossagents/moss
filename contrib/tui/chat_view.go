package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

func (m chatModel) renderHeaderMetaLine() string {
	ctx := m.tuiContext()
	var parts []string
	for _, ext := range m.extensions {
		for _, widget := range ext.HeaderMetaWidgets {
			if seg := strings.TrimSpace(widget(ctx)); seg != "" {
				parts = append(parts, seg)
			}
		}
	}
	if len(parts) == 0 {
		return shellMetaBarStyle.Render("")
	}
	return shellMetaBarStyle.Render(shellHeaderDetailStyle.Render(strings.Join(parts, "  •  ")))
}

func (m chatModel) hasHeaderMetaContent() bool {
	ctx := m.tuiContext()
	for _, ext := range m.extensions {
		for _, widget := range ext.HeaderMetaWidgets {
			if strings.TrimSpace(widget(ctx)) != "" {
				return true
			}
		}
	}
	return false
}

func (m chatModel) renderComposerMetaLine(width int) string {
	label, detail := m.composerMetaSummary()
	line := "  " + label
	if strings.TrimSpace(detail) != "" {
		line += "  •  " + detail
	}
	return composerHintStyle.Width(width).Render(truncateDisplayWidth(line, max(12, width)))
}

func (m chatModel) View() string {
	layout := m.generateLayout()

	// 如果有全屏 overlay（transcript），独占整个抽屉区域
	if overlay := m.activeOverlay(); overlay != nil {
		if overlay.ID() == overlayTranscript {
			overlayView := overlay.View(m, layout.MainWidth, layout.BodyHeight)
			return strings.Join([]string{
				overlayView,
				m.renderStatusPane(layout.Width),
			}, "\n")
		}
		// 其他 overlay 显示在编辑器区域上方
		overlayView := m.renderOverlayPane(layout)
		if strings.TrimSpace(overlayView) != "" {
			return strings.Join([]string{
				overlayView,
				m.renderStatusPane(layout.Width),
			}, "\n")
		}
	}

	var parts []string

	// 流式输出时，在顶部显示当前流式内容的最后几行预览
	if m.streaming {
		if preview := m.renderStreamingPreview(layout.MainWidth); preview != "" {
			parts = append(parts, preview)
		}
	}

	// 编辑器区域
	if editor := m.renderEditorPane(layout); strings.TrimSpace(editor) != "" {
		parts = append(parts, editor)
	}

	// 状态栏
	parts = append(parts, m.renderStatusPane(layout.Width))

	return strings.Join(parts, "\n")
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

func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
