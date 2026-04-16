package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderSelectionListDialogBoundsHeightAndKeepsSelectionVisible(t *testing.T) {
	items := make([]selectionListItem, 0, 16)
	for i := 1; i <= 16; i++ {
		items = append(items, selectionListItem{
			Key:    fmt.Sprintf("item-%02d", i),
			Title:  fmt.Sprintf("Item %02d", i),
			Detail: "summary",
		})
	}
	state := &selectionListState{
		Title:   "Select",
		Footer:  "Footer",
		Message: strings.Join([]string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6"}, "\n"),
		Items:   items,
		Cursor:  11,
	}

	rendered := renderSelectionListDialog(60, 16, state)
	if got := lipgloss.Height(rendered); got > 16 {
		t.Fatalf("dialog height = %d, want <= 16\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "Item 12") {
		t.Fatalf("rendered dialog missing selected item:\n%s", rendered)
	}
	if strings.Contains(rendered, "Item 01") {
		t.Fatalf("rendered dialog should scroll early items out of view:\n%s", rendered)
	}
	if !strings.Contains(rendered, "more") {
		t.Fatalf("rendered dialog should show overflow affordance:\n%s", rendered)
	}
}
