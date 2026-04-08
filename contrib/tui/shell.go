package tui

import (
	"github.com/charmbracelet/lipgloss"
	configpkg "github.com/mossagents/moss/config"
	"path/filepath"
	"strings"
)

func (m chatModel) shellProductTitle() string {
	title := strings.TrimSpace(configpkg.AppName())
	if title == "" {
		title = "moss"
	}
	return title
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
