package tui

import "github.com/charmbracelet/lipgloss"

// 颜色常量
var (
	colorPrimary   = lipgloss.Color("#7C3AED") // 紫色
	colorMuted     = lipgloss.Color("#6B7280") // 灰色
	colorSuccess   = lipgloss.Color("#10B981") // 绿色
	colorError     = lipgloss.Color("#EF4444") // 红色
	colorUser      = lipgloss.Color("#3B82F6") // 蓝色
	colorAssistant = lipgloss.Color("#8B5CF6") // 淡紫
	colorTool      = lipgloss.Color("#F59E0B") // 黄色
)

// 样式定义
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

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

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	systemStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)

	sidebarBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	collapsedToolStyle = lipgloss.NewStyle().
				Foreground(colorTool).
				Italic(true)
)
