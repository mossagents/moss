package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/appkit/product"
	"strings"
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
	const maxItems = 10
	if len(items) > maxItems {
		items = items[:maxItems]
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

// renderSlashPopup 渲染斜杠命令弹窗（显示在输入框上方）。
func (m chatModel) renderSlashPopup(width int) string {
	p := m.slashPopup
	if p == nil || len(p.items) == 0 {
		return ""
	}
	// 弹窗宽度：不超过 88，不小于 48
	popupWidth := min(88, max(48, width-4))

	nameWidth := 22
	summaryWidth := popupWidth - nameWidth - 4

	var rows []string
	for i, item := range p.items {
		num := fmt.Sprintf("%d", i+1)
		if i >= 9 {
			num = "+"
		}
		name := item.name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}
		summary := item.summary
		if summaryWidth > 0 && len(summary) > summaryWidth {
			summary = summary[:summaryWidth-1] + "…"
		}
		var row string
		if i == p.cursor {
			// 选中项：用 › 替代数字，命令名高亮，不使用背景色（避免 ANSI reset 导致颜色混乱）
			cursor := runningStyle.Render("›")
			nameStr := lipgloss.NewStyle().Width(nameWidth).Bold(true).Foreground(colorPrimary).Render(name)
			summaryStr := halfMutedStyle.Render(summary)
			row = fmt.Sprintf(" %s %s  %s", cursor, nameStr, summaryStr)
		} else {
			numStr := halfMutedStyle.Render(num)
			nameStr := lipgloss.NewStyle().Width(nameWidth).Render(name)
			summaryStr := mutedStyle.Render(summary)
			row = fmt.Sprintf(" %s %s  %s", numStr, nameStr, summaryStr)
		}
		rows = append(rows, row)
	}
	footer := composerHintStyle.Render("  ↑↓ navigate  •  Tab/Enter select  •  Esc dismiss")
	rows = append(rows, footer)
	inner := strings.Join(rows, "\n")
	return dialogBoxStyle.Width(popupWidth).Render(inner)
}
