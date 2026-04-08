package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	configpkg "github.com/mossagents/moss/config"
	"strings"
)

type themePickerOption struct {
	name   string
	detail string
}

type themePickerState struct {
	options []themePickerOption
	list    *selectionListState
}

func newThemePickerState(current string) *themePickerState {
	options := []themePickerOption{
		{name: themeDefault, detail: "ANSI semantic colors, works in any terminal."},
		{name: themeDark, detail: "Vibrant hex colors optimized for dark terminals."},
		{name: themePlain, detail: "No colors, plain terminal defaults."},
	}
	items := make([]selectionListItem, 0, len(options))
	state := &themePickerState{
		options: options,
		list: &selectionListState{
			Title:        "Themes",
			Footer:       "↑↓ choose • Enter apply • Esc close",
			EmptyMessage: "No themes available.",
		},
	}
	for i, option := range options {
		items = append(items, selectionListItem{Title: option.name, Detail: option.detail})
		if strings.EqualFold(strings.TrimSpace(option.name), strings.TrimSpace(current)) {
			state.list.Cursor = i
		}
	}
	state.list.Items = items
	return state
}

func (m chatModel) openThemePicker() (chatModel, tea.Cmd) {
	m.themePicker = newThemePickerState(m.theme)
	m.openThemeOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleThemePickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.themePicker == nil || len(m.themePicker.options) == 0 {
		return m.closeThemeOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.themePicker.list.Move(-1)
	case "down":
		m.themePicker.list.Move(1)
	case "enter":
		idx := m.themePicker.list.SelectedIndex()
		if idx >= 0 {
			return m.applyThemePickerSelection(m.themePicker.options[idx])
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) applyThemePickerSelection(option themePickerOption) (chatModel, tea.Cmd) {
	raw := strings.ToLower(strings.TrimSpace(option.name))
	m.theme = raw
	applyTheme(raw)
	if _, err := product.UpdateTUIConfig(func(cfg *configpkg.TUIConfig) error {
		cfg.Theme = raw
		return nil
	}); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("theme switched locally but failed to persist: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switched theme to %s and saved it to config.", raw)})
	}
	m.themePicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayTheme)
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderThemePicker(width int) string {
	if m.themePicker == nil || m.themePicker.list == nil {
		return ""
	}
	return renderSelectionListDialog(width, m.themePicker.list)
}
