package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// skillsPickerItem 表示技能列表中的一个条目。
type skillsPickerItem struct {
	name        string
	description string
	enabled     bool
}

// skillsPopupState 管理 /skills 底部抽屉的状态。
type skillsPopupState struct {
	items  []skillsPickerItem
	cursor int
	height int // TUI 可用高度，用于滚动视口计算
}

func (s *skillsPopupState) move(delta int) {
	if s == nil || len(s.items) == 0 {
		return
	}
	s.cursor += delta
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.items) {
		s.cursor = len(s.items) - 1
	}
}

func (s *skillsPopupState) toggleCurrent() {
	if s == nil || len(s.items) == 0 {
		return
	}
	if s.cursor >= 0 && s.cursor < len(s.items) {
		s.items[s.cursor].enabled = !s.items[s.cursor].enabled
	}
}

func (s *skillsPopupState) selected() skillsPickerItem {
	if s == nil || len(s.items) == 0 {
		return skillsPickerItem{}
	}
	if s.cursor < 0 || s.cursor >= len(s.items) {
		return s.items[0]
	}
	return s.items[s.cursor]
}

// renderSkillsPopup 渲染 /skills 底部抽屉（与输入框同宽，列在输入框下方）。
func (m chatModel) renderSkillsPopup(width int) string {
	s := m.skillsPopup
	if s == nil || len(s.items) == 0 {
		return mutedStyle.Render("  No skills discovered.")
	}

	maxVisible := 10
	if s.height > 6 {
		maxVisible = max(5, min(20, (s.height-4)/2))
	}

	total := len(s.items)
	// 滚动视口：保持 cursor 始终可见
	viewStart := 0
	viewEnd := min(maxVisible, total)
	if s.cursor >= viewEnd {
		viewEnd = s.cursor + 1
		viewStart = viewEnd - maxVisible
		if viewStart < 0 {
			viewStart = 0
		}
	} else if s.cursor < viewStart {
		viewStart = s.cursor
		viewEnd = viewStart + maxVisible
		if viewEnd > total {
			viewEnd = total
		}
	}

	var sb strings.Builder

	// 顶部提示行
	hint := fmt.Sprintf("  ↑↓ 选择  Space 切换  Esc 关闭")
	if total > maxVisible {
		hint += fmt.Sprintf("   %d/%d", s.cursor+1, total)
	}
	sb.WriteString(mutedStyle.Render(hint))
	sb.WriteByte('\n')

	for i := viewStart; i < viewEnd; i++ {
		item := s.items[i]
		checkbox := "[ ]"
		if item.enabled {
			checkbox = lipgloss.NewStyle().Foreground(colorSuccess).Render("[✓]")
		}
		label := item.name
		if item.description != "" {
			label += "  " + mutedStyle.Render(item.description)
		}
		line := fmt.Sprintf("  %s %s", checkbox, label)
		if i == s.cursor {
			line = lipgloss.NewStyle().Width(width).Reverse(true).Render(line)
		}
		sb.WriteString(line)
		if i < viewEnd-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
