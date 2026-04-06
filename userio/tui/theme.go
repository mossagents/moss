package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	themeDefault = "default"
	themeDark    = "dark"
	themePlain   = "plain"
)

func init() {
	applyTheme(resolveThemeName(os.Getenv("MOSSCODE_THEME")))
}

func resolveThemeName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", themeDefault:
		return themeDefault
	case themeDark:
		return themeDark
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
		userMessageStyle = lipgloss.NewStyle()

	case themeDark:
		// 原始 hex 精确色方案，适合暗色终端
		const (
			hexPrimary   = "#D946EF"
			hexSecondary = "#38BDF8"
			hexMuted     = "#6B7280"
			hexHalfMuted = "#4B5563"
			hexSuccess   = "#22C55E"
			hexError     = "#F87171"
			hexTool      = "#F59E0B"
			hexBorder    = "#30363D"
			hexSubtle    = "#94A3B8"
			hexBgPanel   = "#111827"
			hexBgSubtle  = "#161B22"
			hexBgOverlay = "#0F172A"
			hexFgBase    = "#E5E7EB"
			hexBgUserMsg = "#1E293B"
		)
		colorPrimary = lipgloss.Color(hexPrimary)
		colorSecondary = lipgloss.Color(hexSecondary)
		colorSuccess = lipgloss.Color(hexSuccess)
		colorError = lipgloss.Color(hexError)
		colorUser = lipgloss.Color(hexPrimary)
		colorAssistant = lipgloss.Color(hexSecondary)
		colorMuted = lipgloss.AdaptiveColor{Light: hexMuted, Dark: hexMuted}
		colorHalfMuted = lipgloss.AdaptiveColor{Light: hexHalfMuted, Dark: hexHalfMuted}
		colorSubtle = lipgloss.AdaptiveColor{Light: hexSubtle, Dark: hexSubtle}
		colorBorder = lipgloss.AdaptiveColor{Light: hexBorder, Dark: hexBorder}
		colorUserMsgBg = lipgloss.AdaptiveColor{Light: hexBgUserMsg, Dark: hexBgUserMsg}

		baseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexFgBase))
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		shellBrandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		shellHeaderDetailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		shellHeaderSeparatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexSubtle))
		shellHeaderDiagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexPrimary))
		statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		topBarStyle = lipgloss.NewStyle().Padding(0, 1)
		shellRuleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexBorder))
		userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexSecondary))
		toolLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexTool))
		toolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexSuccess))
		toolErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexError))
		progressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted)).Italic(true)
		runningStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexError)).Bold(true)
		systemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted)).Italic(true)
		mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		halfMutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexHalfMuted))
		panelBaseStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgPanel))
		panelMutedStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgSubtle))
		inputBorderStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgPanel)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(hexPrimary)).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgPanel)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(hexBorder)).Padding(1, 2)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexSubtle))
		collapsedToolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexTool)).Italic(true)
		panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexSubtle))
		dialogBoxStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgOverlay)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(hexPrimary)).Padding(1, 2)
		dialogTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
		dialogAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexSecondary))
		dialogHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		dialogItemStyle = lipgloss.NewStyle().Padding(0, 1)
		dialogSelectedItemStyle = lipgloss.NewStyle().Padding(0, 1).Background(lipgloss.Color(hexPrimary)).Foreground(lipgloss.Color("#FFFFFF"))
		composerHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		statusHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
		userMessageStyle = lipgloss.NewStyle()

	default: // themeDefault — ANSI 语义色，亮色/暗色终端均可用
		colorPrimary = lipgloss.Color("5")   // ANSI magenta
		colorSecondary = lipgloss.Color("6") // ANSI cyan
		colorSuccess = lipgloss.Color("2")   // ANSI green
		colorError = lipgloss.Color("1")     // ANSI red
		colorUser = lipgloss.Color("4")
		colorAssistant = lipgloss.Color("6")
		colorMuted = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
		colorHalfMuted = lipgloss.AdaptiveColor{Light: "243", Dark: "247"}
		colorSubtle = lipgloss.AdaptiveColor{Light: "245", Dark: "249"}
		colorBorder = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
		colorUserMsgBg = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}

		baseStyle = lipgloss.NewStyle()
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
		toolLabelStyle = lipgloss.NewStyle().Foreground(colorSecondary)
		toolResultStyle = lipgloss.NewStyle().Foreground(colorSuccess)
		toolErrorStyle = lipgloss.NewStyle().Foreground(colorError)
		progressStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
		runningStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		errorStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
		systemStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
		mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)
		halfMutedStyle = lipgloss.NewStyle().Foreground(colorHalfMuted)
		panelBaseStyle = lipgloss.NewStyle()
		panelMutedStyle = lipgloss.NewStyle()
		inputBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
		sidebarBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
		sidebarTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtle)
		collapsedToolStyle = lipgloss.NewStyle().Foreground(colorSecondary).Italic(true)
		panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtle)
		dialogBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPrimary).Padding(1, 2)
		dialogTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
		dialogAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
		dialogHelpStyle = lipgloss.NewStyle().Foreground(colorMuted)
		dialogItemStyle = lipgloss.NewStyle().Padding(0, 1)
		dialogSelectedItemStyle = lipgloss.NewStyle().Padding(0, 1).Reverse(true)
		composerHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
		statusHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
		userMessageStyle = lipgloss.NewStyle()
	}
}
