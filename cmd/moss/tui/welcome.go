package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// welcomeState 表示欢迎界面的焦点字段。
type welcomeField int

const (
	fieldProvider welcomeField = iota
	fieldWorkspace
	fieldStart
	fieldCount // sentinel
)

// welcomeModel 是欢迎/配置界面。
type welcomeModel struct {
	provider  string
	workspace string
	focus     welcomeField
	width     int
	height    int
	input     textarea.Model // 复用 textarea 作为单行输入
	confirmed bool
	cancelled bool
}

// WelcomeConfig 是欢迎界面收集的配置。
type WelcomeConfig struct {
	Provider  string
	Workspace string
}

func newWelcomeModel(defaultProvider, defaultWorkspace string) welcomeModel {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.SetHeight(1)
	ta.SetWidth(40)
	ta.ShowLineNumbers = false
	ta.CharLimit = 200
	ta.Focus()

	return welcomeModel{
		provider:  defaultProvider,
		workspace: defaultWorkspace,
		focus:     fieldProvider,
		input:     ta,
	}
}

func (m welcomeModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m welcomeModel) Update(msg tea.Msg) (welcomeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "tab", "down":
			m.applyCurrentField()
			m.focus = (m.focus + 1) % fieldCount
			m.syncInput()
			return m, nil

		case "shift+tab", "up":
			m.applyCurrentField()
			m.focus = (m.focus - 1 + fieldCount) % fieldCount
			m.syncInput()
			return m, nil

		case "enter":
			if m.focus == fieldStart {
				m.applyCurrentField()
				m.confirmed = true
				return m, nil
			}
			// 在其他字段上按 Enter 跳到下一个
			m.applyCurrentField()
			m.focus = (m.focus + 1) % fieldCount
			m.syncInput()
			return m, nil
		}
	}

	if m.focus < fieldStart {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *welcomeModel) applyCurrentField() {
	val := strings.TrimSpace(m.input.Value())
	switch m.focus {
	case fieldProvider:
		if val != "" {
			m.provider = val
		}
	case fieldWorkspace:
		if val != "" {
			m.workspace = val
		}
	}
}

func (m *welcomeModel) syncInput() {
	switch m.focus {
	case fieldProvider:
		m.input.SetValue(m.provider)
		m.input.Focus()
	case fieldWorkspace:
		m.input.SetValue(m.workspace)
		m.input.Focus()
	case fieldStart:
		m.input.Blur()
	}
}

func (m welcomeModel) View() string {
	var b strings.Builder

	// Logo
	logo := titleStyle.Render("🌿 moss")
	version := mutedStyle.Render(" v" + appVersion)
	b.WriteString("\n" + logo + version + "\n\n")

	// Fields
	fields := []struct {
		label string
		value string
		field welcomeField
	}{
		{"Provider", m.provider, fieldProvider},
		{"Workspace", m.workspace, fieldWorkspace},
	}

	for _, f := range fields {
		label := f.label
		if m.focus == f.field {
			label = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("▸ " + label)
			b.WriteString(fmt.Sprintf("  %s\n", label))
			b.WriteString(fmt.Sprintf("  %s\n\n", m.input.View()))
		} else {
			label = mutedStyle.Render("  " + label)
			b.WriteString(fmt.Sprintf("  %s: %s\n\n", label, f.value))
		}
	}

	// Start button
	if m.focus == fieldStart {
		startBtn := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorPrimary).
			Padding(0, 2).
			Render("[ Start ]")
		b.WriteString(fmt.Sprintf("  %s\n", startBtn))
	} else {
		b.WriteString(fmt.Sprintf("  %s\n", mutedStyle.Render("[ Start ]")))
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("  Tab/↑↓ 切换  Enter 确认  Esc 退出"))
	b.WriteString("\n")

	return b.String()
}

func (m welcomeModel) config() WelcomeConfig {
	return WelcomeConfig{
		Provider:  m.provider,
		Workspace: m.workspace,
	}
}
