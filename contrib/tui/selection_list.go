package tui

import (
	"strings"
)

type selectionListItem struct {
	Key    string
	Title  string
	Detail string
}

type selectionListState struct {
	Title        string
	Footer       string
	EmptyMessage string
	Message      string
	Items        []selectionListItem
	Cursor       int
	MultiSelect  bool
	Marked       map[string]struct{}
}

func (s *selectionListState) Move(delta int) {
	if s == nil || len(s.Items) == 0 || delta == 0 {
		return
	}
	s.Cursor += delta
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	if s.Cursor >= len(s.Items) {
		s.Cursor = len(s.Items) - 1
	}
}

func (s *selectionListState) SelectedIndex() int {
	if s == nil || len(s.Items) == 0 {
		return -1
	}
	if s.Cursor < 0 {
		return 0
	}
	if s.Cursor >= len(s.Items) {
		return len(s.Items) - 1
	}
	return s.Cursor
}

func (s *selectionListState) Selected() *selectionListItem {
	idx := s.SelectedIndex()
	if idx < 0 {
		return nil
	}
	return &s.Items[idx]
}

func (s *selectionListState) itemKey(item selectionListItem) string {
	key := strings.TrimSpace(item.Key)
	if key != "" {
		return key
	}
	return strings.TrimSpace(item.Title)
}

func (s *selectionListState) IsSelected(idx int) bool {
	if s == nil || !s.MultiSelect || idx < 0 || idx >= len(s.Items) {
		return false
	}
	if len(s.Marked) == 0 {
		return false
	}
	_, ok := s.Marked[s.itemKey(s.Items[idx])]
	return ok
}

func (s *selectionListState) ToggleSelected() {
	idx := s.SelectedIndex()
	if s == nil || !s.MultiSelect || idx < 0 {
		return
	}
	if s.Marked == nil {
		s.Marked = make(map[string]struct{})
	}
	key := s.itemKey(s.Items[idx])
	if _, ok := s.Marked[key]; ok {
		delete(s.Marked, key)
		return
	}
	s.Marked[key] = struct{}{}
}

func (s *selectionListState) SelectedKeys() []string {
	if s == nil || len(s.Marked) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.Marked))
	for _, item := range s.Items {
		key := s.itemKey(item)
		if _, ok := s.Marked[key]; ok {
			out = append(out, key)
		}
	}
	return out
}

func renderSelectionListDialog(width int, state *selectionListState) string {
	if state == nil {
		return ""
	}
	if width < 40 {
		width = 40
	}
	var body strings.Builder
	if len(state.Items) == 0 {
		body.WriteString(mutedStyle.Render(valueOrDefaultString(state.EmptyMessage, "No items available.")))
	} else {
		for i, item := range state.Items {
			prefix := "  "
			if i == state.SelectedIndex() {
				prefix = "› "
			}
			line := prefix
			if state.MultiSelect {
				mark := "[ ] "
				if state.IsSelected(i) {
					mark = "[x] "
				}
				line += mark
			}
			line += item.Title
			if strings.TrimSpace(item.Detail) != "" {
				line += "  " + mutedStyle.Render(item.Detail)
			}
			if i == state.SelectedIndex() {
				body.WriteString(dialogSelectedItemStyle.Render(line))
			} else {
				body.WriteString(dialogItemStyle.Render(line))
			}
			body.WriteString("\n")
		}
		if selected := state.Selected(); selected != nil && strings.TrimSpace(selected.Detail) != "" {
			body.WriteString("\n")
			body.WriteString(dialogAccentStyle.Render("Selected"))
			body.WriteString("\n")
			body.WriteString(wrapText(selected.Detail, width-4))
		}
	}
	if strings.TrimSpace(state.Message) != "" {
		body.WriteString("\n\n")
		body.WriteString(mutedStyle.Render(state.Message))
	}
	return renderDialogFrame(width, valueOrDefaultString(state.Title, "Select"), []string{strings.TrimSpace(body.String())}, valueOrDefaultString(state.Footer, "↑↓ move  •  Enter confirm  •  Esc close"))
}
