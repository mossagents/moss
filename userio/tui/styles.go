package tui

import "github.com/charmbracelet/lipgloss"

// ANSI 语义色常量（default 主题使用）。
// 使用 ANSI 16 色，在亮色/暗色终端下均可正常显示。
// dark 主题使用 hex 精确色（在 theme.go 中切换）。
var (
	colorPrimary   = lipgloss.Color("5") // ANSI magenta
	colorSecondary = lipgloss.Color("6") // ANSI cyan
	colorSuccess   = lipgloss.Color("2") // ANSI green
	colorError     = lipgloss.Color("1") // ANSI red
	colorUser      = lipgloss.Color("5") // ANSI magenta
	colorAssistant = lipgloss.Color("6") // ANSI cyan

	// 自适应灰度色：在亮色终端偏暗，在暗色终端偏亮
	colorMuted     = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	colorHalfMuted = lipgloss.AdaptiveColor{Light: "243", Dark: "247"}
	colorSubtle    = lipgloss.AdaptiveColor{Light: "245", Dark: "249"}
	colorBorder    = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}

	// user message 背景：极浅灰（亮色终端）/ 极深灰（暗色终端）
	colorUserMsgBg = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}
)

// 样式定义（随主题切换，见 theme.go）
var (
	baseStyle = lipgloss.NewStyle()

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

	// tool 使用 cyan（与 assistant 同色族）+ 非粗体，视觉上低于 assistant
	toolLabelStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	progressStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	runningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

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

	// 不设置背景色：由终端主题控制
	panelBaseStyle  = lipgloss.NewStyle()
	panelMutedStyle = lipgloss.NewStyle()

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)

	sidebarBoxStyle = lipgloss.NewStyle().
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
				Foreground(colorSecondary).
				Italic(true)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSubtle)

	dialogBoxStyle = lipgloss.NewStyle().
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

	// 选中项：使用 Reverse 在亮色/暗色终端下均有良好对比
	dialogSelectedItemStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Reverse(true)

	composerHintStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	statusHintStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// 用户消息背景高亮（P9）
	userMessageStyle = lipgloss.NewStyle().
				Background(colorUserMsgBg)
)
