package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	themeDefault = "default"
	themePlain   = "plain"
)

func init() {
	applyTheme(resolveThemeName(os.Getenv("MOSSCODE_THEME")))
}

func resolveThemeName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", themeDefault:
		return themeDefault
	case themePlain:
		return themePlain
	default:
		return themeDefault
	}
}

func applyTheme(name string) {
	switch resolveThemeName(name) {
	case themePlain:
		baseStyle = lipgloss.NewStyle()
		titleStyle = lipgloss.NewStyle().Bold(true)
		shellBrandStyle = lipgloss.NewStyle().Bold(true)
		shellTitleStyle = lipgloss.NewStyle().Bold(true)
		shellHeaderDetailStyle = lipgloss.NewStyle()
		shellHeaderSeparatorStyle = lipgloss.NewStyle()
		shellHeaderDiagStyle = lipgloss.NewStyle()
		statusBarStyle = lipgloss.NewStyle()
		topBarStyle = lipgloss.NewStyle().Padding(0, 1)
		shellRuleStyle = lipgloss.NewStyle()
		userLabelStyle = lipgloss.NewStyle().Bold(true)
		assistantLabelStyle = lipgloss.NewStyle().Bold(true)
		toolLabelStyle = lipgloss.NewStyle()
		toolResultStyle = lipgloss.NewStyle()
		toolErrorStyle = lipgloss.NewStyle().Bold(true)
		progressStyle = lipgloss.NewStyle().Italic(true)
		runningStyle = lipgloss.NewStyle().Bold(true)
		errorStyle = lipgloss.NewStyle().Bold(true)
		systemStyle = lipgloss.NewStyle().Italic(true)
		mutedStyle = lipgloss.NewStyle()
		halfMutedStyle = lipgloss.NewStyle()
		panelBaseStyle = lipgloss.NewStyle()
		panelMutedStyle = lipgloss.NewStyle()
		inputBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true)
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true)
		collapsedToolStyle = lipgloss.NewStyle().Italic(true)
		panelTitleStyle = lipgloss.NewStyle().Bold(true)
		dialogBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
		dialogTitleStyle = lipgloss.NewStyle().Bold(true)
		dialogAccentStyle = lipgloss.NewStyle().Bold(true)
		dialogHelpStyle = lipgloss.NewStyle()
		dialogItemStyle = lipgloss.NewStyle().Padding(0, 1)
		dialogSelectedItemStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
		composerHintStyle = lipgloss.NewStyle()
		statusHintStyle = lipgloss.NewStyle()
	default:
		baseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		shellBrandStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		shellHeaderDetailStyle = lipgloss.NewStyle().Foreground(colorMuted)
		shellHeaderSeparatorStyle = lipgloss.NewStyle().Foreground(colorSubtle)
		shellHeaderDiagStyle = lipgloss.NewStyle().Foreground(colorPrimary)
		statusBarStyle = lipgloss.NewStyle().Foreground(colorMuted)
		topBarStyle = lipgloss.NewStyle().Padding(0, 1)
		shellRuleStyle = lipgloss.NewStyle().Foreground(colorBorder)
		userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorUser)
		assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAssistant)
		toolLabelStyle = lipgloss.NewStyle().Foreground(colorTool)
		toolResultStyle = lipgloss.NewStyle().Foreground(colorSuccess)
		toolErrorStyle = lipgloss.NewStyle().Foreground(colorError)
		progressStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
		runningStyle = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
		errorStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
		systemStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
		mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)
		halfMutedStyle = lipgloss.NewStyle().Foreground(colorHalfMuted)
		panelBaseStyle = lipgloss.NewStyle().Background(colorBgPanel)
		panelMutedStyle = lipgloss.NewStyle().Background(colorBgSubtle)
		inputBorderStyle = lipgloss.NewStyle().Background(colorBgPanel).Border(lipgloss.RoundedBorder()).BorderForeground(colorPrimary).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Background(colorBgPanel).Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtle)
		collapsedToolStyle = lipgloss.NewStyle().Foreground(colorTool).Italic(true)
		panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtle)
		dialogBoxStyle = lipgloss.NewStyle().Background(colorBgOverlay).Border(lipgloss.RoundedBorder()).BorderForeground(colorPrimary).Padding(1, 2)
		dialogTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		dialogAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
		dialogHelpStyle = lipgloss.NewStyle().Foreground(colorMuted)
		dialogItemStyle = lipgloss.NewStyle().Padding(0, 1)
		dialogSelectedItemStyle = lipgloss.NewStyle().Padding(0, 1).Background(colorPrimary).Foreground(lipgloss.Color("#FFFFFF"))
		composerHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
		statusHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
	}
}
