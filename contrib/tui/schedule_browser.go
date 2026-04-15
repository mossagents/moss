package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/runtime/scheduling"
	"strings"
)

type scheduleBrowserState struct {
	items   []scheduling.ScheduleItem
	cursor  int
	message string
}

func newScheduleBrowserState(items []scheduling.ScheduleItem) *scheduleBrowserState {
	return &scheduleBrowserState{items: items}
}

func (m chatModel) handleScheduleBrowserKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.scheduleBrowser == nil {
		return m, nil
	}
	switch msg.String() {
	case "up":
		if m.scheduleBrowser.cursor > 0 {
			m.scheduleBrowser.cursor--
		}
	case "down":
		if m.scheduleBrowser.cursor < len(m.scheduleBrowser.items)-1 {
			m.scheduleBrowser.cursor++
		}
	case "e":
		return m.runSelectedScheduleNow()
	case "r":
		return m.refreshScheduleBrowser()
	case "d":
		return m.deleteSelectedSchedule()
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) refreshScheduleBrowser() (chatModel, tea.Cmd) {
	if m.scheduleCtrl == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Schedule listing is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	items, err := m.scheduleCtrl.List()
	if err != nil {
		if m.scheduleBrowser != nil {
			m.scheduleBrowser.message = fmt.Sprintf("Refresh failed: %v", err)
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to refresh schedules: %v", err)})
		}
		m.refreshViewport()
		return m, nil
	}
	if m.scheduleBrowser == nil {
		m.openScheduleOverlay(items)
	} else {
		m.scheduleBrowser.items = items
		if m.scheduleBrowser.cursor >= len(items) && len(items) > 0 {
			m.scheduleBrowser.cursor = len(items) - 1
		}
		if len(items) == 0 {
			m.scheduleBrowser.cursor = 0
		}
		m.scheduleBrowser.message = "Schedules refreshed."
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) runSelectedScheduleNow() (chatModel, tea.Cmd) {
	if m.scheduleBrowser == nil || len(m.scheduleBrowser.items) == 0 {
		return m, nil
	}
	if m.scheduleCtrl == nil {
		m.scheduleBrowser.message = "Immediate execution is unavailable."
		m.refreshViewport()
		return m, nil
	}
	selected := m.scheduleBrowser.items[m.scheduleBrowser.cursor]
	out, err := m.scheduleCtrl.RunNow(selected.ID)
	if err != nil {
		m.scheduleBrowser.message = fmt.Sprintf("Run failed: %v", err)
	} else {
		m.scheduleBrowser.message = out
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) deleteSelectedSchedule() (chatModel, tea.Cmd) {
	if m.scheduleBrowser == nil || len(m.scheduleBrowser.items) == 0 {
		return m, nil
	}
	if m.scheduleCtrl == nil {
		m.scheduleBrowser.message = "Schedule deletion is unavailable."
		m.refreshViewport()
		return m, nil
	}
	selected := m.scheduleBrowser.items[m.scheduleBrowser.cursor]
	out, err := m.scheduleCtrl.Cancel(selected.ID)
	if err != nil {
		m.scheduleBrowser.message = fmt.Sprintf("Delete failed: %v", err)
		m.refreshViewport()
		return m, nil
	}
	items, listErr := []scheduling.ScheduleItem(nil), error(nil)
	if m.scheduleCtrl != nil {
		items, listErr = m.scheduleCtrl.List()
	}
	if listErr == nil && m.scheduleCtrl != nil {
		m.scheduleBrowser.items = items
		if m.scheduleBrowser.cursor >= len(items) && len(items) > 0 {
			m.scheduleBrowser.cursor = len(items) - 1
		}
		if len(items) == 0 {
			m.scheduleBrowser.cursor = 0
		}
	}
	m.scheduleBrowser.message = out
	if listErr != nil {
		m.scheduleBrowser.message = fmt.Sprintf("%s (refresh failed: %v)", out, listErr)
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderScheduleBrowser(width int) string {
	if m.scheduleBrowser == nil {
		return ""
	}
	if width < 40 {
		width = 40
	}
	var sb strings.Builder
	if len(m.scheduleBrowser.items) == 0 {
		sb.WriteString(mutedStyle.Render("No scheduled jkobs."))
		sb.WriteString("\n")
	} else {
		for i, item := range m.scheduleBrowser.items {
			prefix := "  "
			if i == m.scheduleBrowser.cursor {
				prefix = "▸ "
			}
			line := fmt.Sprintf("%s%s | %s", prefix, item.ID, item.Schedule)
			if item.NextRun != "" {
				line += " | next: " + item.NextRun
			}
			if i == m.scheduleBrowser.cursor {
				sb.WriteString(dialogSelectedItemStyle.Render(line))
			} else {
				sb.WriteString(dialogItemStyle.Render(line))
			}
			sb.WriteString("\n")
		}
		selected := m.scheduleBrowser.items[m.scheduleBrowser.cursor]
		sb.WriteString("\n")
		sb.WriteString(dialogAccentStyle.Render("Selected"))
		sb.WriteString("\n")
		sb.WriteString("  ID: " + selected.ID + "\n")
		sb.WriteString("  Schedule: " + selected.Schedule + "\n")
		if selected.NextRun != "" {
			sb.WriteString("  Next run: " + selected.NextRun + "\n")
		}
		if selected.LastRun != "" {
			sb.WriteString("  Last run: " + selected.LastRun + "\n")
		}
		if selected.RunCount > 0 {
			sb.WriteString(fmt.Sprintf("  Run count: %d\n", selected.RunCount))
		}
		if strings.TrimSpace(selected.Goal) != "" {
			sb.WriteString("\n")
			sb.WriteString(wrapText(selected.Goal, width-4))
			sb.WriteString("\n")
		}
	}
	if strings.TrimSpace(m.scheduleBrowser.message) != "" {
		sb.WriteString("\n")
		sb.WriteString(mutedStyle.Render(m.scheduleBrowser.message))
	}
	return renderDialogFrame(width, "Schedules", []string{strings.TrimSpace(sb.String())}, "↑↓ choose • e run now • d delete • r refresh • Esc close")
}
