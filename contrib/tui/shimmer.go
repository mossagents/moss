package tui

import (
	"github.com/charmbracelet/lipgloss"
	"math"
	"os"
	"strings"
	"time"
)

// shimmerText 对给定文字渲染一个随时间移动的"扫光"高亮动画。
// 基于当前时间计算高亮位置，模拟 Codex CLI 的 streaming 指示器效果。
// 在不支持 256 色的终端（如 xterm-16 色）下，降级为 Bold 渲染。
func shimmerText(text string, now time.Time) string {
	if !terminalSupports256Color() {
		return lipgloss.NewStyle().Bold(true).Render(text)
	}
	chars := []rune(text)
	if len(chars) == 0 {
		return ""
	}
	// 扫光周期：每 1.4 秒完整扫一遍
	period := float64(len(chars) + 8)
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := now.UnixNano() / int64(time.Millisecond)
	pos := math.Mod(float64(elapsed)/1400.0*period, period)

	var b strings.Builder
	for i, c := range chars {
		dist := math.Abs(float64(i) - pos)
		var s lipgloss.Style
		switch {
		case dist < 1.0:
			s = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		case dist < 2.5:
			s = lipgloss.NewStyle().Foreground(colorPrimary)
		default:
			s = lipgloss.NewStyle().Foreground(colorMuted)
		}
		b.WriteString(s.Render(string(c)))
	}
	return b.String()
}

// terminalSupports256Color 检测终端是否支持 256 色（或更高）。
// 方法：检查 $TERM 和 $COLORTERM 环境变量。
func terminalSupports256Color() bool {
	colorterm := strings.ToLower(os.Getenv("COLORTERM"))
	if colorterm == "truecolor" || colorterm == "24bit" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	return strings.Contains(term, "256") || strings.Contains(term, "color") ||
		strings.Contains(term, "xterm") || strings.HasPrefix(term, "screen")
}
