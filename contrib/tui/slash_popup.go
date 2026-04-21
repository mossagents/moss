package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/harness/appkit/product"
)

// slashPopupItem 是弹窗中的一个候选命令项。
type slashPopupItem struct {
	name    string
	summary string
}

// slashPopupState 管理斜杠命令弹窗的显示状态。
type slashPopupState struct {
	items  []slashPopupItem
	cursor int
}

func (s *slashPopupState) selected() slashPopupItem {
	if s == nil || len(s.items) == 0 {
		return slashPopupItem{}
	}
	if s.cursor < 0 || s.cursor >= len(s.items) {
		return s.items[0]
	}
	return s.items[s.cursor]
}

func (s *slashPopupState) move(delta int) {
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

// buildSlashPopup constructs a new slashPopupState from the given input text,
// custom commands, and discovered skills. Returns nil when no popup is needed.
// Results are sorted: prefix matches first, then fuzzy (subsequence) matches.
func buildSlashPopup(input string, customCommands []product.CustomCommand, discoveredSkills []string) *slashPopupState {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return nil
	}
	lower := strings.ToLower(input)
	var prefixItems, fuzzyItems []slashPopupItem
	seen := make(map[string]struct{}, 32)

	addItem := func(name, summary string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		item := slashPopupItem{name: name, summary: summary}
		if strings.HasPrefix(name, lower) {
			prefixItems = append(prefixItems, item)
		} else if fuzzyMatchSlash(name, lower) {
			fuzzyItems = append(fuzzyItems, item)
		}
	}

	for _, cmd := range slashCommandCatalog {
		if !cmd.HiddenInNav {
			addItem(cmd.Name, cmd.Summary)
		}
	}
	for _, cmd := range customCommands {
		addItem("/"+cmd.Name, "Custom command")
	}
	for _, skillName := range discoveredSkills {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(skillName), "/")
		if name != "/" {
			addItem(name, "Skill")
		}
	}

	items := append(prefixItems, fuzzyItems...)
	if len(items) == 0 {
		return nil
	}
	return &slashPopupState{items: items, cursor: 0}
}

// fuzzyMatchSlash 对斜杠命令名执行 fuzzy 子序列匹配（不含前缀匹配，前缀匹配由调用方处理）。
func fuzzyMatchSlash(cmdName, query string) bool {
	if query == "/" {
		return true
	}
	q := strings.TrimPrefix(query, "/")
	c := strings.TrimPrefix(cmdName, "/")
	return fuzzyContainsStr(c, q)
}

// fuzzyContainsStr 判断 s 中是否含有 pattern 的所有字符（按序）。
func fuzzyContainsStr(s, pattern string) bool {
	if pattern == "" {
		return true
	}
	pi := 0
	patRunes := []rune(pattern)
	for _, c := range s {
		if pi < len(patRunes) && c == patRunes[pi] {
			pi++
		}
	}
	return pi == len(patRunes)
}

// renderSlashPopup 渲染斜杠命令弹窗（Claude Code 底部抽屉风格：无边框内联列表，支持滚动）。
func (m chatModel) renderSlashPopup(width int) string {
	p := m.slashPopup
	if p == nil || len(p.items) == 0 {
		return ""
	}

	// 限制最大显示行数：最多 10 行
	total := len(p.items)
	maxVisible := min(10, max(1, total))

	listWidth := min(width, max(48, width))
	nameWidth := 24
	summaryWidth := listWidth - nameWidth - 6
	if summaryWidth < 10 {
		summaryWidth = 0
	}

	// 计算滚动视口：确保 cursor 始终可见
	viewSize := min(maxVisible, total)
	viewStart := 0
	if p.cursor >= viewStart+viewSize {
		viewStart = p.cursor - viewSize + 1
	}
	if p.cursor < viewStart {
		viewStart = p.cursor
	}
	viewEnd := viewStart + viewSize

	var rows []string
	for i := viewStart; i < viewEnd; i++ {
		item := p.items[i]
		name := item.name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}
		summary := item.summary
		if summaryWidth > 0 && len(summary) > summaryWidth {
			summary = summary[:summaryWidth-1] + "…"
		}

		if i == p.cursor {
			cursor := "›"
			var line string
			if summaryWidth > 0 && strings.TrimSpace(summary) != "" {
				line = fmt.Sprintf(" %s %-*s  %s", cursor, nameWidth, name, summary)
			} else {
				line = fmt.Sprintf(" %s %-*s", cursor, nameWidth, name)
			}
			rows = append(rows, lipgloss.NewStyle().Width(listWidth).Reverse(true).Render(line))
		} else {
			nameStr := lipgloss.NewStyle().Width(nameWidth + 3).Render(fmt.Sprintf("   %s", name))
			var line string
			if summaryWidth > 0 && strings.TrimSpace(summary) != "" {
				line = nameStr + "  " + halfMutedStyle.Render(summary)
			} else {
				line = nameStr
			}
			rows = append(rows, lipgloss.NewStyle().Width(listWidth).Render(line))
		}
	}

	// 顶部：分隔线 + 计数（多于 viewSize 时显示滚动提示）
	var header string
	if total > viewSize {
		countStr := halfMutedStyle.Render(fmt.Sprintf("%d/%d", p.cursor+1, total))
		sep := halfMutedStyle.Render(strings.Repeat("─", listWidth-8))
		header = sep + "  " + countStr
	} else {
		header = halfMutedStyle.Render(strings.Repeat("─", listWidth))
	}
	return header + "\n" + strings.Join(rows, "\n")
}
