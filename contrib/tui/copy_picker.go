package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// isCopyableMessage reports whether a message should appear in the copy picker
// and be eligible for /copy. This is the single source of truth for both.
func isCopyableMessage(msg chatMessage) bool {
	switch msg.kind {
	case msgAssistant, msgSystem, msgToolResult, msgToolError:
		return strings.TrimSpace(msg.content) != ""
	}
	return false
}

type copyPickerItem struct {
	content string
}

type copyPickerState struct {
	items []copyPickerItem
	list  *selectionListState
}

func newCopyPickerState(messages []chatMessage) *copyPickerState {
	var items []copyPickerItem
	var listItems []selectionListItem
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !isCopyableMessage(msg) {
			continue
		}
		content := strings.TrimSpace(msg.content)
		var label string
		switch msg.kind {
		case msgAssistant:
			label = "AI"
		case msgToolResult:
			if name, _ := msg.meta["tool"].(string); name != "" {
				label = fmt.Sprintf("Tool: %s", name)
			} else {
				label = "Tool result"
			}
		case msgToolError:
			if name, _ := msg.meta["tool"].(string); name != "" {
				label = fmt.Sprintf("Tool error: %s", name)
			} else {
				label = "Tool error"
			}
		case msgSystem:
			label = "System"
		}
		items = append(items, copyPickerItem{content: content})
		listItems = append(listItems, selectionListItem{
			Title:  label,
			Detail: previewText(content, 300),
		})
		if len(items) >= 20 {
			break
		}
	}
	return &copyPickerState{
		items: items,
		list: &selectionListState{
			Title:        "Copy message",
			Footer:       "↑↓ choose  •  y / Enter copy  •  Esc close",
			EmptyMessage: "No messages available to copy.",
			Items:        listItems,
		},
	}
}

// previewText returns s truncated to maxRunes, with "…" appended if truncated.
// Newlines are collapsed to spaces for single-line display.
func previewText(s string, maxRunes int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", ""))
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

type copyPickerOverlayDialog struct{}

func (copyPickerOverlayDialog) ID() overlayID { return overlayCopy }

func (copyPickerOverlayDialog) View(m chatModel, width, _ int) string {
	if m.copyPicker == nil {
		return ""
	}
	return renderSelectionListDialog(width, m.copyPicker.list)
}

func (copyPickerOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	return m.handleCopyPickerKey(msg)
}

func (m chatModel) handleCopyPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.copyPicker == nil {
		return m.closeCopyOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.copyPicker.list.Move(-1)
		m.refreshViewport()
	case "down":
		m.copyPicker.list.Move(1)
		m.refreshViewport()
	case "y", "enter":
		idx := m.copyPicker.list.SelectedIndex()
		if idx >= 0 && idx < len(m.copyPicker.items) {
			content := m.copyPicker.items[idx].content
			m = m.closeCopyOverlay()
			if err := writeClipboard(content); err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to copy: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Copied to clipboard."})
			}
			m.refreshViewport()
		}
	case "esc":
		return m.closeCopyOverlay(), nil
	}
	return m, nil
}
