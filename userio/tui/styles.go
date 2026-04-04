package tui

import "github.com/charmbracelet/lipgloss"

// 颜色常量
var (
	colorPrimary   = lipgloss.Color("#D946EF")
	colorMuted     = lipgloss.Color("#6B7280")
	colorSuccess   = lipgloss.Color("#22C55E")
	colorError     = lipgloss.Color("#F87171")
	colorUser      = lipgloss.Color("#22C55E")
	colorAssistant = lipgloss.Color("#38BDF8")
	colorTool      = lipgloss.Color("#F59E0B")
	colorBorder    = lipgloss.Color("#30363D")
	colorSubtle    = lipgloss.Color("#94A3B8")
)

// 样式定义
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	shellTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	topBarStyle = lipgloss.NewStyle().
			Padding(0, 1)

	shellRuleStyle = lipgloss.NewStyle().
			Foreground(colorBorder)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorUser)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAssistant)

	toolLabelStyle = lipgloss.NewStyle().
			Foreground(colorTool)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	progressStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	runningStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	systemStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)

	sidebarBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSubtle)

	sidebarSectionTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(colorAssistant)

	collapsedToolStyle = lipgloss.NewStyle().
				Foreground(colorTool).
				Italic(true)
)
