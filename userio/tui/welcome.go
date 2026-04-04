package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	config "github.com/mossagents/moss/config"
)

// welcomeState 表示欢迎界面的焦点字段。
type welcomeField int

const (
	fieldProvider welcomeField = iota
	fieldProviderName
	fieldModel
	fieldWorkspace
	fieldStart
	fieldCount // sentinel
)

// welcomeModel 是欢迎/配置界面。
type welcomeModel struct {
	provider     string
	providerName string
	banner       string
	model        string
	workspace    string
	focus        welcomeField
	width        int
	height       int
	input        textarea.Model // 复用 textarea 作为单行输入
	confirmed    bool
	cancelled    bool
	now          func() tea.Msg
}

// WelcomeConfig 是欢迎界面收集的配置。
type WelcomeConfig struct {
	ProviderName string
	Provider     string
	Model        string
	Workspace    string
}

func newWelcomeModel(defaultProvider, defaultProviderName, defaultModel, defaultWorkspace, banner string) welcomeModel {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.SetHeight(1)
	ta.SetWidth(40)
	ta.ShowLineNumbers = false
	ta.CharLimit = 200
	ta.Focus()

	return welcomeModel{
		provider:     defaultProvider,
		providerName: defaultProviderName,
		banner:       strings.TrimRight(banner, "\r\n"),
		model:        defaultModel,
		workspace:    defaultWorkspace,
		focus:        fieldProvider,
		input:        ta,
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
	case fieldProviderName:
		if val != "" {
			m.providerName = val
		}
	case fieldModel:
		m.model = val // 允许为空，表示使用 provider 默认模型
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
	case fieldProviderName:
		m.input.SetValue(m.providerName)
		m.input.Focus()
	case fieldModel:
		m.input.SetValue(m.model)
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
	if m.banner != "" {
		b.WriteString("\n" + titleStyle.Render(m.banner) + "\n")
		b.WriteString(mutedStyle.Render(" v"+appVersion) + "\n\n")
	} else {
		logo := titleStyle.Render("mosscode")
		version := mutedStyle.Render(" v" + appVersion)
		b.WriteString("\n" + logo + version + "\n\n")
	}

	// Fields
	modelDisplay := m.model
	if modelDisplay == "" {
		modelDisplay = "(default)"
	}
	fields := []struct {
		label string
		value string
		field welcomeField
	}{
		{"Provider", m.provider, fieldProvider},
		{"Provider Name", m.providerName, fieldProviderName},
		{"Model", modelDisplay, fieldModel},
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
	b.WriteString(mutedStyle.Render("  Tab/↑↓ switch  Enter confirm  Esc quit  Ctrl+C quit"))
	b.WriteString("\n")

	return b.String()
}

func (m welcomeModel) config() WelcomeConfig {
	identity := config.NormalizeProviderIdentity("", m.provider, m.providerName)
	return WelcomeConfig{
		ProviderName: identity.Name,
		Provider:     identity.Provider,
		Model:        m.model,
		Workspace:    m.workspace,
	}
}

// saveWelcomeConfig 将用户选择的配置（provider, name, model）持久化到 ~/.moss/config.yaml。
// 仅更新模型连接相关字段，保留已有的 api_key, base_url, skills 等配置。
func saveWelcomeConfig(wCfg WelcomeConfig) {
	cfgPath := config.DefaultGlobalConfigPath()
	if cfgPath == "" {
		return
	}
	existing, _ := config.LoadConfig(cfgPath)
	identity := config.NormalizeProviderIdentity("", wCfg.Provider, wCfg.ProviderName)
	existing.Name = identity.Name
	existing.Provider = identity.Provider
	existing.Model = wCfg.Model
	_ = config.SaveConfig(cfgPath, existing)
}
