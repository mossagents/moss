package tui

import (
	"github.com/charmbracelet/lipgloss"
	configpkg "github.com/mossagents/moss/config"
	"path/filepath"
	"strings"
)

func (m chatModel) shellProductTitle() string {
	return compactShellBrandTitle(configpkg.AppName())
}

func compactShellBrandTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "moss"
	}
	lower := strings.ToLower(title)
	switch {
	case lower == "chat", lower == "assistant", lower == "shell":
		return "moss"
	case strings.HasPrefix(lower, "moss"):
		return "moss"
	default:
		return title
	}
}

func (m chatModel) renderShellHeader() string {
	inner := max(1, m.width-topBarStyle.GetHorizontalFrameSize())

	// Right: provider (model)
	var rightRaw string
	if provider := strings.TrimSpace(m.provider); provider != "" {
		if mdl := strings.TrimSpace(m.model); mdl != "" && !m.modelAuto {
			rightRaw = provider + " (" + mdl + ")"
		} else {
			rightRaw = provider
		}
	}
	right := shellHeaderDetailStyle.Render(rightRaw)
	rightW := lipgloss.Width(right)

	// Left: workspace-basename [branch]
	wsBase := filepath.Base(valueOrDefaultString(strings.TrimSpace(m.workspace), "."))
	if wsBase == "." || wsBase == "" {
		wsBase = "moss"
	}
	branch := strings.TrimSpace(m.gitBranch)
	// Reserve at least 1 gap between left and right.
	maxLeftW := max(8, inner-rightW-1)
	var left string
	wsStyled := shellBrandStyle.Render(wsBase)
	if branch != "" {
		branchStyled := shellHeaderDetailStyle.Render(" [" + branch + "]")
		combined := wsStyled + branchStyled
		if lipgloss.Width(combined) <= maxLeftW {
			left = combined
		} else {
			avail := max(4, maxLeftW-lipgloss.Width(branchStyled))
			left = shellBrandStyle.Render(truncateDisplayWidth(wsBase, avail)) + branchStyled
		}
	} else {
		if lipgloss.Width(wsStyled) > maxLeftW {
			wsStyled = shellBrandStyle.Render(truncateDisplayWidth(wsBase, maxLeftW))
		}
		left = wsStyled
	}

	leftW := lipgloss.Width(left)
	gapW := max(0, inner-leftW-rightW)
	gap := strings.Repeat(" ", gapW)
	return topBarStyle.Width(max(1, m.width)).Render(left + gap + right)
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
