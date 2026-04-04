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
		titleStyle = lipgloss.NewStyle().Bold(true)
		shellTitleStyle = lipgloss.NewStyle().Bold(true)
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
		inputBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Padding(0, 1)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true)
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true)
		collapsedToolStyle = lipgloss.NewStyle().Italic(true)
	default:
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
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
		inputBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorPrimary).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorBorder).Padding(0, 1)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtle)
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAssistant)
		collapsedToolStyle = lipgloss.NewStyle().Foreground(colorTool).Italic(true)
	}
}
