package tui

import (
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	config "github.com/mossagents/moss/harness/config"
	"strings"
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
	ta.Prompt = ""
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
	headerTitle := "Ready when you are"
	headerBody := "Connect a model, pick your workspace, and start from the shared moss shell."
	var hero strings.Builder
	if m.banner != "" {
		hero.WriteString(titleStyle.Render(m.banner))
		hero.WriteString("\n")
	}
	hero.WriteString(shellTitleStyle.Render(headerTitle))
	hero.WriteString("\n")
	hero.WriteString(mutedStyle.Render(headerBody))
	hero.WriteString("\n")
	hero.WriteString(halfMutedStyle.Render("v" + appVersion))

	var fieldsBody strings.Builder
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
			label = dialogAccentStyle.Render("▸ " + label)
			fieldsBody.WriteString(fmt.Sprintf("%s\n", label))
			fieldsBody.WriteString(fmt.Sprintf("%s\n\n", inputBorderStyle.Render(m.input.View())))
		} else {
			label = mutedStyle.Render(label)
			fieldsBody.WriteString(fmt.Sprintf("%s\n%s\n\n", label, baseStyle.Render(f.value)))
		}
	}

	startText := dialogItemStyle.Render("[ Start ]")
	if m.focus == fieldStart {
		startText = dialogSelectedItemStyle.Render("[ Start ]")
	}
	fieldsBody.WriteString(startText)

	help := dialogHelpStyle.Render("Tab/↑↓ switch • Enter confirm • Esc quit • Ctrl+C quit")
	leftWidth := m.width / 2
	if leftWidth < 42 {
		leftWidth = 42
	}
	if leftWidth > 64 {
		leftWidth = 64
	}
	rightWidth := m.width - leftWidth - 2
	if rightWidth < 32 {
		rightWidth = 32
	}
	left := renderDialogFrame(leftWidth, "Welcome", []string{hero.String()}, "")
	right := renderDialogFrame(rightWidth, "Session setup", []string{strings.TrimSpace(fieldsBody.String())}, "")
	layout := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	return strings.Join([]string{
		topBarStyle.Render(shellHeaderDetailStyle.Render("moss setup")),
		shellRuleStyle.Width(max(1, m.width)).Render(strings.Repeat("─", max(1, m.width-1))),
		layout,
		help,
	}, "\n")
}

func (m welcomeModel) config() WelcomeConfig {
	identity := config.NormalizeProviderIdentity(m.provider, m.providerName)
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
	identity := config.NormalizeProviderIdentity(wCfg.Provider, wCfg.ProviderName)
	existing.Name = identity.Name
	existing.Provider = identity.Provider
	existing.Model = wCfg.Model
	_ = config.SaveConfig(cfgPath, existing)
}
