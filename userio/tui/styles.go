package tui

import "github.com/charmbracelet/lipgloss"

// 颜色常量
var (
	colorPrimary   = lipgloss.Color("#D946EF")
	colorSecondary = lipgloss.Color("#38BDF8")
	colorMuted     = lipgloss.Color("#6B7280")
	colorHalfMuted = lipgloss.Color("#4B5563")
	colorSuccess   = lipgloss.Color("#22C55E")
	colorError     = lipgloss.Color("#F87171")
	colorUser      = lipgloss.Color("#D946EF")
	colorAssistant = lipgloss.Color("#38BDF8")
	colorTool      = lipgloss.Color("#F59E0B")
	colorBorder    = lipgloss.Color("#30363D")
	colorSubtle    = lipgloss.Color("#94A3B8")
	colorBgBase    = lipgloss.Color("#0D1117")
	colorBgPanel   = lipgloss.Color("#111827")
	colorBgSubtle  = lipgloss.Color("#161B22")
	colorBgOverlay = lipgloss.Color("#0F172A")
)

// 样式定义
var (
	baseStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	shellBrandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	shellTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	shellHeaderDetailStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	shellHeaderSeparatorStyle = lipgloss.NewStyle().
					Foreground(colorSubtle)

	shellHeaderDiagStyle = lipgloss.NewStyle().
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

	halfMutedStyle = lipgloss.NewStyle().
			Foreground(colorHalfMuted)

	panelBaseStyle = lipgloss.NewStyle().
			Background(colorBgPanel)

	panelMutedStyle = lipgloss.NewStyle().
			Background(colorBgSubtle)

	inputBorderStyle = lipgloss.NewStyle().
				Background(colorBgPanel).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)

	sidebarBoxStyle = lipgloss.NewStyle().
			Background(colorBgPanel).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	sidebarSectionTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(colorSubtle)

	collapsedToolStyle = lipgloss.NewStyle().
				Foreground(colorTool).
				Italic(true)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSubtle)

	dialogBoxStyle = lipgloss.NewStyle().
			Background(colorBgOverlay).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 2)

	dialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	dialogAccentStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSecondary)

	dialogHelpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	dialogItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	dialogSelectedItemStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Background(colorPrimary).
				Foreground(lipgloss.Color("#FFFFFF"))

	composerHintStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	statusHintStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)
