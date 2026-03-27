package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// msgKind 区分消息类型。
type msgKind int

const (
	msgUser msgKind = iota
	msgAssistant
	msgSystem
	msgToolStart
	msgToolResult
	msgToolError
	msgProgress
	msgError
)

// chatMessage 表示聊天区域中的一条消息。
type chatMessage struct {
	kind    msgKind
	content string
}

var markdownRendererCache sync.Map // map[int]*glamour.TermRenderer

// renderMessage 渲染单条消息为带样式的字符串。
func renderMessage(m chatMessage, width int) string {
	maxContent := width - 4
	if maxContent < 20 {
		maxContent = 20
	}

	switch m.kind {
	case msgUser:
		label := userLabelStyle.Render("You")
		return fmt.Sprintf("\n%s\n%s", label, renderMarkdown(m.content, maxContent))

	case msgAssistant:
		label := assistantLabelStyle.Render("🤖 moss")
		return fmt.Sprintf("\n%s\n%s", label, renderMarkdown(m.content, maxContent))

	case msgSystem:
		return systemStyle.Render(fmt.Sprintf("\n  ● %s", renderMarkdown(m.content, maxContent)))

	case msgToolStart:
		return toolLabelStyle.Render(fmt.Sprintf("  🔧 %s", m.content))

	case msgToolResult:
		text := m.content
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		return toolResultStyle.Render(fmt.Sprintf("  ✅ %s", renderMarkdown(text, maxContent)))

	case msgToolError:
		return toolErrorStyle.Render(fmt.Sprintf("  ❌ %s", m.content))

	case msgProgress:
		return progressStyle.Render(fmt.Sprintf("  ⏳ %s", m.content))

	case msgError:
		return errorStyle.Render(fmt.Sprintf("Error: %s", m.content))

	default:
		return m.content
	}
}

func renderMarkdown(content string, width int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	renderer, err := markdownRenderer(width)
	if err != nil {
		return wrapText(content, width)
	}
	out, err := renderer.Render(content)
	if err != nil {
		return wrapText(content, width)
	}
	return strings.TrimRight(out, "\n")
}

func markdownRenderer(width int) (*glamour.TermRenderer, error) {
	if v, ok := markdownRendererCache.Load(width); ok {
		return v.(*glamour.TermRenderer), nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	markdownRendererCache.Store(width, r)
	return r, nil
}

// isToolMsg 判断消息是否属于可折叠的工具类别。
func isToolMsg(kind msgKind) bool {
	return kind == msgToolStart || kind == msgToolResult
}

// renderAllMessages 渲染所有消息为单个可滚动字符串。
// 当 toolCollapsed 为 true 时，连续的工具消息会折叠为一行摘要。
func renderAllMessages(messages []chatMessage, width int, toolCollapsed bool) string {
	if len(messages) == 0 {
		return mutedStyle.Render("\n  Type a message to start...\n")
	}
	var b strings.Builder
	i := 0
	for i < len(messages) {
		m := messages[i]
		if toolCollapsed && isToolMsg(m.kind) {
			// 计算连续工具消息数量
			count := 0
			for j := i; j < len(messages) && isToolMsg(messages[j].kind); j++ {
				count++
			}
			b.WriteString(collapsedToolStyle.Render(
				fmt.Sprintf("  ▶ %d tool calls (Ctrl+T to expand)", count)))
			b.WriteString("\n")
			i += count
		} else {
			b.WriteString(renderMessage(m, width))
			b.WriteString("\n")
			i++
		}
	}
	return b.String()
}

// wrapText 简单的文本换行。
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}
		for len(line) > width {
			// 找到最后一个空格以在单词边界处换行
			idx := strings.LastIndex(line[:width], " ")
			if idx <= 0 {
				idx = width
			}
			result.WriteString(line[:idx])
			result.WriteString("\n")
			line = strings.TrimLeft(line[idx:], " ")
		}
		if len(line) > 0 {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	return strings.TrimRight(result.String(), "\n")
}
