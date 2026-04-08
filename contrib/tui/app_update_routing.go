package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 全局窗口大小
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = ws.Width
		m.height = ws.Height
	}

	switch m.state {
	case stateWelcome:
		return m.updateWelcome(msg)
	case stateChat:
		return m.updateChat(msg)
	}
	return m, nil
}
