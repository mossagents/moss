package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/appkit/product"
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
func buildSlashPopup(input string, customCommands []product.CustomCommand, discoveredSkills []string) *slashPopupState {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return nil
	}
	lower := strings.ToLower(input)
	items := make([]slashPopupItem, 0, 16)
	seen := make(map[string]struct{}, 32)

	for _, cmd := range slashCommandCatalog {
		if cmd.HiddenInNav {
			continue
		}
		if !fuzzyMatchSlash(cmd.Name, lower) {
			continue
		}
		if _, ok := seen[cmd.Name]; ok {
			continue
		}
		seen[cmd.Name] = struct{}{}
		items = append(items, slashPopupItem{name: cmd.Name, summary: cmd.Summary})
	}
	for _, cmd := range customCommands {
		name := "/" + cmd.Name
		if !fuzzyMatchSlash(name, lower) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		items = append(items, slashPopupItem{name: name, summary: "Custom command"})
	}
	for _, skillName := range discoveredSkills {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(skillName), "/")
		if name == "/" {
			continue
		}
		if !fuzzyMatchSlash(name, lower) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		items = append(items, slashPopupItem{name: name, summary: "Skill"})
	}
	if len(items) == 0 {
		return nil
	}
	const maxItems = 10
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return &slashPopupState{items: items, cursor: 0}
}

// fuzzyMatchSlash 对斜杠命令名执行 prefix + fuzzy 匹配。
// query 包含前导 "/"（如 "/mo"）。
func fuzzyMatchSlash(cmdName, query string) bool {
	if query == "/" {
		return true // 刚输入 "/" 时显示所有命令
	}
	if strings.HasPrefix(cmdName, query) {
		return true // 精确前缀优先
	}
	// fuzzy：query 的每个字符在 cmdName 中按序出现
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
	// 弹窗宽度：不超过 60，不小于 36
	popupWidth := min(60, max(36, width-4))

	nameWidth := 20
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
		numStr := halfMutedStyle.Render(num)
		nameStr := lipgloss.NewStyle().Width(nameWidth).Render(name)
		summaryStr := mutedStyle.Render(summary)
		row := fmt.Sprintf(" %s %s  %s", numStr, nameStr, summaryStr)
		if i == p.cursor {
			row = dialogSelectedItemStyle.Width(popupWidth).Render(row)
		}
		rows = append(rows, row)
	}
	footer := composerHintStyle.Render("  ↑↓ navigate  •  Tab/Enter select  •  Esc dismiss")
	rows = append(rows, footer)
	inner := strings.Join(rows, "\n")
	return dialogBoxStyle.Width(popupWidth).Render(inner)
}
