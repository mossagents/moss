package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	configpkg "github.com/mossagents/moss/config"
	"path/filepath"
	"strings"
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
	brand := shellBrandStyle.Render(m.shellProductTitle())
	details := []string{}
	if cwd := strings.TrimSpace(valueOrDefaultString(m.workspace, ".")); cwd != "" {
		details = append(details, shellHeaderDetailStyle.Render(filepath.Base(cwd)))
	}
	if provider := strings.TrimSpace(m.provider); provider != "" {
		if model := strings.TrimSpace(m.model); model != "" {
			details = append(details, shellHeaderDetailStyle.Render(provider+" · "+model))
		} else {
			details = append(details, shellHeaderDetailStyle.Render(provider))
		}
	}
	if len(details) == 0 {
		details = append(details, shellHeaderDetailStyle.Render("not connected"))
	}
	maxDetailWidth := max(12, m.width-lipgloss.Width(brand)-8)
	detailText := truncateDisplayWidth(strings.Join(details, shellHeaderSeparatorStyle.Render(" • ")), maxDetailWidth)
	available := m.width - lipgloss.Width(brand) - lipgloss.Width(detailText) - 2
	if available < 3 {
		available = 3
	}
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		brand,
		" ",
		shellHeaderDiagStyle.Render(strings.Repeat("╱", available)),
		" ",
		detailText,
	)
	return strings.Join([]string{
		topBarStyle.Width(max(1, m.width)).Render(header),
		shellRuleStyle.Width(max(1, m.width)).Render(strings.Repeat("─", max(1, m.width-1))),
	}, "\n")
}

func (m chatModel) renderShellSidebar(width int) string {
	if width < 24 {
		width = 24
	}
	contentWidth := width - sidebarBoxStyle.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	var blocks []string
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
		blocks = append(blocks, renderSidebarSection(title, body))
	}
	blocks = append(blocks, renderSidebarSection("Session", m.renderShellSessionMeta(width-8)))
	blocks = append(blocks, renderSidebarSection("Shortcuts", m.renderShellShortcutSummary(width-8)))
	body := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return sidebarBoxStyle.Width(contentWidth).Render(body)
}

func renderShellPanel(width int, title, body string) string {
	if width < 24 {
		width = 24
	}
	contentWidth := width - sidebarBoxStyle.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "-"
	}
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(baseStyle.Render(body))
	return sidebarBoxStyle.Width(contentWidth).Render(b.String())
}

func renderSidebarSection(title, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "-"
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sidebarSectionTitleStyle.Render(title),
		baseStyle.Render(body),
	)
}

func renderDialogFrame(width int, title string, body []string, footer string) string {
	if width < 40 {
		width = 40
	}
	contentWidth := width - dialogBoxStyle.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	content := make([]string, 0, len(body)+2)
	content = append(content, dialogTitleStyle.Render(title))
	content = append(content, strings.Join(body, "\n"))
	if strings.TrimSpace(footer) != "" {
		content = append(content, dialogHelpStyle.Render(footer))
	}
	return dialogBoxStyle.Width(contentWidth).Render(strings.TrimSpace(strings.Join(content, "\n\n")))
}

func (m chatModel) renderShellSessionMeta(width int) string {
	lines := []string{
		fmt.Sprintf("state     %s", valueOrDefaultRunState(m.streaming)),
		fmt.Sprintf("thread    %s", valueOrDefaultString(m.currentSessionID, "(new)")),
		fmt.Sprintf("profile   %s", valueOrDefaultString(m.profile, "default")),
		fmt.Sprintf("trust     %s", valueOrDefaultString(m.trust, "trusted")),
		fmt.Sprintf("approval  %s", valueOrDefaultString(m.approvalMode, "confirm")),
		fmt.Sprintf("theme     %s", valueOrDefaultString(m.theme, themeDefault)),
		fmt.Sprintf("messages  %d", len(m.messages)),
	}
	if m.fastMode {
		lines = append(lines, "mode      fast")
	}
	if progress := strings.TrimSpace(m.progress.renderLine(m.now(), width)); progress != "" {
		lines = append(lines, "", halfMutedStyle.Render(progress))
	}
	return wrapText(strings.Join(lines, "\n"), width)
}

func (m chatModel) renderShellShortcutSummary(width int) string {
	lines := []string{
		"/help /status /profile",
		"@ file mentions · / slash commands",
		"Tab complete · Shift+Tab switch mode",
		"Esc Esc cancel · Ctrl+C clear/quit",
	}
	return wrapText(strings.Join(lines, "\n"), width)
}
