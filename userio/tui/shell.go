package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	configpkg "github.com/mossagents/moss/config"
)

func (m chatModel) shellProductTitle() string {
	title := strings.TrimSpace(m.sidebarTitle)
	if title == "" {
		title = strings.TrimSpace(configpkg.AppName())
	}
	if title == "" {
		title = "moss"
	}
	return title
}

func (m chatModel) shellSidebarVisible() bool {
	return m.shellSidebarWidth() > 0
}

func (m chatModel) shellSidebarWidth() int {
	if m.width < 140 {
		return 0
	}
	width := m.width / 4
	if width < 30 {
		width = 30
	}
	if width > 38 {
		width = 38
	}
	return width
}

func (m chatModel) shellMainGapWidth() int {
	if m.shellSidebarVisible() {
		return 2
	}
	return 0
}

func (m chatModel) renderShellHeader() string {
	product := shellTitleStyle.Render(m.shellProductTitle())
	conn := strings.TrimSpace(m.provider)
	if strings.TrimSpace(m.model) != "" {
		conn += " (" + strings.TrimSpace(m.model) + ")"
	}
	if ws := valueOrDefaultString(m.workspace, "."); ws != "." && ws != "" {
		conn += " · " + filepath.Base(ws)
	}
	if conn == "" {
		conn = "(not connected)"
	}

	var b strings.Builder
	b.WriteString(topBarStyle.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Center, product, statusBarStyle.Render("  "+conn)),
	))
	b.WriteString("\n")
	b.WriteString(shellRuleStyle.Width(m.width).Render(strings.Repeat("─", max(1, m.width-1))))
	return b.String()
}

func (m chatModel) renderShellSidebar(width int) string {
	if width < 24 {
		width = 24
	}
	var sections []string

	if strings.TrimSpace(m.sidebarTitle) != "" || m.renderSidebarFn != nil {
		title := strings.TrimSpace(m.sidebarTitle)
		if title == "" {
			title = m.shellProductTitle()
		}
		body := ""
		if m.renderSidebarFn != nil {
			body = strings.TrimSpace(m.renderSidebarFn())
		}
		if body == "" {
			body = "No product-specific context is available yet."
		} else {
			body = strings.TrimSpace(renderMarkdown(body, width-6))
		}
		sections = append(sections, renderShellPanel(width, title, body))
	}

	sections = append(sections, renderShellPanel(width, "Session", m.renderShellSessionMeta(width-6)))
	sections = append(sections, renderShellPanel(width, "Shortcuts", m.renderShellShortcutSummary(width-6)))
	return lipgloss.NewStyle().Width(width).Render(strings.Join(sections, "\n\n"))
}

func renderShellPanel(width int, title, body string) string {
	if width < 24 {
		width = 24
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "-"
	}
	var b strings.Builder
	b.WriteString(sidebarTitleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(body)
	return sidebarBoxStyle.Width(width).Render(b.String())
}

func (m chatModel) renderShellSessionMeta(width int) string {
	lines := []string{
		fmt.Sprintf("state      %s", valueOrDefaultRunState(m.streaming)),
		fmt.Sprintf("thread     %s", valueOrDefaultString(m.currentSessionID, "(new)")),
		fmt.Sprintf("profile    %s", valueOrDefaultString(m.profile, "default")),
		fmt.Sprintf("trust      %s", valueOrDefaultString(m.trust, "trusted")),
		fmt.Sprintf("approval   %s", valueOrDefaultString(m.approvalMode, "confirm")),
		fmt.Sprintf("theme      %s", valueOrDefaultString(m.theme, themeDefault)),
		fmt.Sprintf("messages   %d", len(m.messages)),
	}
	if m.fastMode {
		lines = append(lines, "mode       fast")
	}
	if progress := strings.TrimSpace(m.progress.renderLine(m.now(), width)); progress != "" {
		lines = append(lines, "", progress)
	}
	return wrapText(strings.Join(lines, "\n"), width)
}

func (m chatModel) renderShellShortcutSummary(width int) string {
	lines := []string{
		"/help, /status, /profile",
		"@ file mentions, / slash commands",
		"Tab complete, Shift+Tab switch mode",
		"Esc Esc cancel, Ctrl+C clear/quit",
	}
	return wrapText(strings.Join(lines, "\n"), width)
}
