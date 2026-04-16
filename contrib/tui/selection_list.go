package tui

import (
	"github.com/charmbracelet/lipgloss"
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

const (
	selectionListCompactMaxHeight   = 18
	selectionListDefaultVisibleRows = 8
)

func (m chatModel) selectionDialogMaxHeight() int {
	if m.height <= 0 {
		return 0
	}
	layout := m.generateLayout()
	if layout.BodyHeight <= 0 {
		return 0
	}
	limit := min(layout.BodyHeight-1, selectionListCompactMaxHeight)
	if limit < 10 {
		return layout.BodyHeight
	}
	return limit
}

func renderSelectionListDialog(width, maxHeight int, state *selectionListState) string {
	if state == nil {
		return ""
	}
	if maxHeight <= 0 {
		return renderSelectionListDialogWithLimits(width, state, len(state.Items), -1)
	}
	visibleRows := min(len(state.Items), selectionListDefaultVisibleRows)
	detailLines := 5
	rendered := renderSelectionListDialogWithLimits(width, state, visibleRows, detailLines)
	for lipgloss.Height(rendered) > maxHeight {
		switch {
		case detailLines > 0:
			detailLines--
		case visibleRows > 1:
			visibleRows--
		default:
			return rendered
		}
		rendered = renderSelectionListDialogWithLimits(width, state, visibleRows, detailLines)
	}
	return rendered
}

func renderSelectionListDialogWithLimits(width int, state *selectionListState, visibleRows, detailLines int) string {
	if state == nil {
		return ""
	}
	if width < 40 {
		width = 40
	}
	contentWidth := width - dialogBoxStyle.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	listBlock := renderSelectionListItems(state, visibleRows)
	detailBlock := renderSelectionListDetail(contentWidth, state, detailLines)
	sections := make([]string, 0, 2)
	if strings.TrimSpace(listBlock) != "" {
		sections = append(sections, listBlock)
	}
	if strings.TrimSpace(detailBlock) != "" {
		sections = append(sections, detailBlock)
	}
	body := strings.Join(sections, "\n\n")
	return renderDialogFrame(width, valueOrDefaultString(state.Title, "Select"), []string{strings.TrimSpace(body)}, valueOrDefaultString(state.Footer, "↑↓ move  •  Enter confirm  •  Esc close"))
}

func renderSelectionListItems(state *selectionListState, visibleRows int) string {
	if state == nil {
		return ""
	}
	if len(state.Items) == 0 {
		return mutedStyle.Render(valueOrDefaultString(state.EmptyMessage, "No items available."))
	}
	selected := state.SelectedIndex()
	start, end := selectionListVisibleRange(len(state.Items), selected, visibleRows)
	lines := make([]string, 0, end-start+2)
	if start > 0 {
		lines = append(lines, mutedStyle.Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		item := state.Items[i]
		prefix := "  "
		if i == selected {
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
		if i == selected {
			lines = append(lines, dialogSelectedItemStyle.Render(line))
		} else {
			lines = append(lines, dialogItemStyle.Render(line))
		}
	}
	if end < len(state.Items) {
		lines = append(lines, mutedStyle.Render("  ↓ more"))
	}
	return strings.Join(lines, "\n")
}

func renderSelectionListDetail(width int, state *selectionListState, detailLines int) string {
	if state == nil || detailLines == 0 {
		return ""
	}
	if message := trimSelectionListText(strings.TrimSpace(state.Message), detailLines); message != "" {
		return mutedStyle.Render(message)
	}
	selected := state.Selected()
	if selected == nil || strings.TrimSpace(selected.Detail) == "" {
		return ""
	}
	detail := trimSelectionListText(wrapText(selected.Detail, max(1, width-4)), detailLines)
	if detail == "" {
		return ""
	}
	return dialogAccentStyle.Render("Selected") + "\n" + detail
}

func selectionListVisibleRange(total, selected, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if limit <= 0 || limit >= total {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > total {
		end = total
		start = max(0, end-limit)
	}
	return start, end
}

func trimSelectionListText(text string, maxLines int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxLines < 0 {
		return text
	}
	if maxLines == 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	lines = append([]string(nil), lines[:maxLines]...)
	last := strings.TrimRight(lines[len(lines)-1], " ")
	if last == "" {
		last = "…"
	} else {
		last += " …"
	}
	lines[len(lines)-1] = last
	return strings.Join(lines, "\n")
}
