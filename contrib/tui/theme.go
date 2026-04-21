package tui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

const (
	themeDefault = "default"
	themeDark    = "dark"
	themePlain   = "plain"
)

// themeMu guards applyTheme to prevent concurrent style mutation
// (e.g. when multiple embedded TUI instances or tests call applyTheme).
var themeMu sync.Mutex

func init() {
	applyTheme(os.Getenv("MOSSCODE_THEME"))
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
	themeMu.Lock()
	defer themeMu.Unlock()

	switch resolveThemeName(name) {
	case themePlain:
		applyPlainTheme()

	case themeDark:
		applyDarkTheme()

	default:
		applyDefaultTheme()
	}
}

func applyPlainTheme() {
	baseStyle = lipgloss.NewStyle()
	titleStyle = lipgloss.NewStyle().Bold(true)
	shellBrandStyle = lipgloss.NewStyle().Bold(true)
	shellTitleStyle = lipgloss.NewStyle().Bold(true)
	shellHeaderDetailStyle = lipgloss.NewStyle()
	shellHeaderSeparatorStyle = lipgloss.NewStyle()
	shellMetaBarStyle = lipgloss.NewStyle().Padding(0, 1)
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
	panelMutedStyle = lipgloss.NewStyle()
	statusBarStyle = lipgloss.NewStyle().Padding(0, 1)
	statusAccentStyle = lipgloss.NewStyle().Bold(true)
	inputBorderStyle = lipgloss.NewStyle().PaddingLeft(2)
	composerBoxStyle = lipgloss.NewStyle().PaddingLeft(2)
	dialogBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	dialogTitleStyle = lipgloss.NewStyle().Bold(true)
	dialogAccentStyle = lipgloss.NewStyle().Bold(true)
	dialogHelpStyle = lipgloss.NewStyle()
	eventSummaryStyle = lipgloss.NewStyle()
	eventDetailStyle = lipgloss.NewStyle()
	eventPendingStyle = lipgloss.NewStyle()
	eventSuccessStyle = lipgloss.NewStyle().Bold(true)
	eventErrorStyle = lipgloss.NewStyle().Bold(true)
	dialogSelectedItemStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	composerHintStyle = lipgloss.NewStyle()
	statusHintStyle = lipgloss.NewStyle()
	setCommonThemeStyles()
}

func applyDarkTheme() {
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

	baseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexFgBase))
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
	shellBrandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
	shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
	shellHeaderDetailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
	shellHeaderSeparatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexSubtle))
	shellMetaBarStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color(hexMuted))
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
	panelMutedStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgSubtle)).Foreground(lipgloss.Color(hexFgBase)).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color(hexBorder)).Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgSubtle)).Border(lipgloss.NormalBorder(), true, false, false, false).BorderForeground(lipgloss.Color(hexBorder)).Padding(0, 1)
	statusAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
	inputBorderStyle = lipgloss.NewStyle().PaddingLeft(2)
	composerBoxStyle = lipgloss.NewStyle().PaddingLeft(2)
	dialogBoxStyle = lipgloss.NewStyle().Background(lipgloss.Color(hexBgOverlay)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(hexBorder)).Padding(1, 2)
	dialogTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexPrimary))
	dialogAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hexSecondary))
	dialogHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
	eventSummaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
	eventDetailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexSubtle))
	eventPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexTool))
	eventSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexSuccess))
	eventErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexError))
	dialogSelectedItemStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#1F2937"))
	composerHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
	statusHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(hexMuted))
	setCommonThemeStyles()
}

func applyDefaultTheme() {
	colorPrimary = lipgloss.Color("5")
	colorSecondary = lipgloss.Color("6")
	colorSuccess = lipgloss.Color("2")
	colorError = lipgloss.Color("1")
	colorUser = lipgloss.Color("4")
	colorAssistant = lipgloss.Color("6")
	colorMuted = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	colorHalfMuted = lipgloss.AdaptiveColor{Light: "243", Dark: "247"}
	colorSubtle = lipgloss.AdaptiveColor{Light: "245", Dark: "249"}
	colorBorder = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}

	baseStyle = lipgloss.NewStyle()
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	shellBrandStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	shellHeaderDetailStyle = lipgloss.NewStyle().Foreground(colorMuted)
	shellHeaderSeparatorStyle = lipgloss.NewStyle().Foreground(colorSubtle)
	shellMetaBarStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(colorMuted)
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
	panelMutedStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(colorBorder).Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, false).BorderForeground(colorBorder).Padding(0, 1)
	statusAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	inputBorderStyle = lipgloss.NewStyle().PaddingLeft(2)
	composerBoxStyle = lipgloss.NewStyle().PaddingLeft(2)
	dialogBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
	dialogTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	dialogAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
	dialogHelpStyle = lipgloss.NewStyle().Foreground(colorMuted)
	eventSummaryStyle = lipgloss.NewStyle().Foreground(colorMuted)
	eventDetailStyle = lipgloss.NewStyle().Foreground(colorHalfMuted)
	eventPendingStyle = lipgloss.NewStyle().Foreground(colorSecondary)
	eventSuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	eventErrorStyle = lipgloss.NewStyle().Foreground(colorError)
	dialogSelectedItemStyle = lipgloss.NewStyle().Padding(0, 1).Reverse(true)
	composerHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
	statusHintStyle = lipgloss.NewStyle().Foreground(colorMuted)
	setCommonThemeStyles()
}

func setCommonThemeStyles() {
	topBarStyle = lipgloss.NewStyle().Padding(0, 1)
	shellMetaBarStyle = shellMetaBarStyle.Padding(0, 1)
	dialogItemStyle = lipgloss.NewStyle().Padding(0, 1)
	userMessageStyle = lipgloss.NewStyle()
}
